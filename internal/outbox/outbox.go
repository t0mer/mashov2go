// Package outbox delivers queued events to family members. It takes due rows
// from the deliveries table, sends them through the member's connector, and
// applies the retry policy from CLAUDE.md §8.5.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/t0mer/mashov2go/internal/connector"
	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/metrics"
	"github.com/t0mer/mashov2go/internal/store"
)

// MaxAttempts is the number of send attempts on one channel before the
// delivery fails (or switches to the fallback channel).
const MaxAttempts = 6

// backoff is the wait after the nth failed attempt: 30 s, 2 m, 10 m, then 1 h.
var backoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour}

// Backoff returns the wait after the given number of failed attempts (n >= 1).
func Backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > len(backoff) {
		return backoff[len(backoff)-1]
	}
	return backoff[attempts-1]
}

// ErrNoChannel means the member cannot be reached on the requested channel
// (no phone, no bound chat, no enabled connector).
var ErrNoChannel = errors.New("no usable channel")

// Resolver maps a member and channel to a connector and the address to send to.
// The caller closes the returned connector.
type Resolver interface {
	Resolve(ctx context.Context, m family.Member, channel string) (connector.Connector, string, error)
}

// Worker drains due deliveries. It is safe to run RunOnce from one goroutine.
type Worker struct {
	Store    *store.Store
	Resolver Resolver
	Now      func() time.Time
	Batch    int
	Log      *slog.Logger
	// Deliveries counts outcomes by channel and status (nil disables counting).
	Deliveries *metrics.Counter
}

// mark counts a delivery outcome once its store update has succeeded.
func (w *Worker) mark(channel, status string, err error) error {
	if err == nil {
		w.Deliveries.Inc(channel, status)
	}
	return err
}

// RunOnce processes up to Batch due deliveries and returns how many it handled.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	now := w.now()
	batch := w.Batch
	if batch <= 0 {
		batch = 50
	}
	due, err := w.Store.DueDeliveries(ctx, now, batch)
	if err != nil {
		return 0, fmt.Errorf("loading due deliveries: %w", err)
	}
	for _, d := range due {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := w.process(ctx, d); err != nil {
			// A store failure is not a send failure; stop so the next run retries it.
			return 0, err
		}
	}
	return len(due), nil
}

func (w *Worker) process(ctx context.Context, d store.Delivery) error {
	m, err := w.Store.GetMember(ctx, d.MemberID)
	if errors.Is(err, store.ErrNotFound) {
		return w.mark(d.Channel, "failed", w.Store.MarkDeliveryFailed(ctx, d.ID, d.Attempts, "member removed"))
	}
	if err != nil {
		return err
	}
	if !m.Active {
		return w.mark(d.Channel, "skipped", w.Store.MarkDeliverySkipped(ctx, d.ID, "member inactive"))
	}

	conn, addr, err := w.Resolver.Resolve(ctx, m, d.Channel)
	if errors.Is(err, ErrNoChannel) {
		// Try the fallback before giving up; a fallback is used at most once.
		if ok, ferr := w.fallback(ctx, d, m); ferr != nil || ok {
			return ferr
		}
		return w.mark(d.Channel, "skipped", w.Store.MarkDeliverySkipped(ctx, d.ID, "no usable channel"))
	}
	attempts := d.Attempts + 1
	if err != nil {
		// A broken connector config is treated as a failed attempt, so it
		// still ends at MaxAttempts instead of retrying forever.
		return w.failed(ctx, d, m, attempts, false, err)
	}
	defer func() { _ = conn.Close() }()

	sendErr := conn.SendTo(ctx, addr, connector.Message{Text: d.Body, EventID: strconv.FormatInt(d.EventID, 10)})
	if sendErr == nil {
		return w.mark(d.Channel, "sent", w.Store.MarkDeliverySent(ctx, d.ID, attempts))
	}
	w.log().Warn("delivery failed", "delivery", d.ID, "channel", d.Channel, "attempt", attempts, "error", sendErr.Error())
	return w.failed(ctx, d, m, attempts, connector.IsPermanent(sendErr), sendErr)
}

// failed applies the retry policy to one failed attempt: permanent errors and
// exhausted attempts go to the fallback channel or end as failed; anything else
// is retried after the backoff, or the provider's Retry-After if that is longer.
func (w *Worker) failed(ctx context.Context, d store.Delivery, m family.Member, attempts int, permanentErr bool, err error) error {
	if permanentErr || attempts >= MaxAttempts {
		if ok, ferr := w.fallback(ctx, d, m); ferr != nil || ok {
			return ferr
		}
		return w.mark(d.Channel, "failed", w.Store.MarkDeliveryFailed(ctx, d.ID, attempts, err.Error()))
	}
	wait := Backoff(attempts)
	if ra := connector.RetryAfterOf(err); ra > wait {
		wait = ra
	}
	return w.mark(d.Channel, "retry", w.Store.MarkDeliveryRetry(ctx, d.ID, attempts, w.now().Add(wait), err.Error()))
}

// fallback moves the delivery to the member's fallback channel. It reports false
// when there is no fallback to use.
func (w *Worker) fallback(ctx context.Context, d store.Delivery, m family.Member) (bool, error) {
	if m.FallbackChannel == "" || m.FallbackChannel == d.Channel {
		return false, nil
	}
	return w.Store.SwitchDeliveryChannel(ctx, d.ID, d.Channel, m.FallbackChannel, w.now())
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Worker) log() *slog.Logger {
	if w.Log != nil {
		return w.Log
	}
	return slog.Default()
}

// StoreResolver finds the connector for a member from the stored connectors.
type StoreResolver struct {
	Store *store.Store
	HTTP  *http.Client
}

// Resolve implements Resolver. WhatsApp uses the member's connector, or the
// default WhatsApp connector when none is set. Telegram uses the first enabled bot.
func (r StoreResolver) Resolve(ctx context.Context, m family.Member, channel string) (connector.Connector, string, error) {
	switch channel {
	case family.ChannelWhatsApp:
		if m.WhatsAppPhone == "" {
			return nil, "", ErrNoChannel
		}
		c, err := r.findConnector(ctx, m.WhatsAppConnectorID, false)
		if err != nil {
			return nil, "", err
		}
		conn, err := r.build(ctx, c)
		return conn, m.WhatsAppPhone, err
	case family.ChannelTelegram:
		if m.TelegramChatID == 0 {
			return nil, "", ErrNoChannel
		}
		c, err := r.findConnector(ctx, 0, true)
		if err != nil {
			return nil, "", err
		}
		conn, err := r.build(ctx, c)
		return conn, strconv.FormatInt(m.TelegramChatID, 10), err
	default:
		return nil, "", ErrNoChannel
	}
}

// findConnector returns an enabled connector. For WhatsApp (telegram=false) it
// uses id, or the default WhatsApp connector when id is 0.
func (r StoreResolver) findConnector(ctx context.Context, id int64, telegram bool) (store.Connector, error) {
	if telegram {
		all, err := r.Store.ListConnectors(ctx)
		if err != nil {
			return store.Connector{}, err
		}
		for _, c := range all {
			if c.Type == "telegram" && c.Enabled {
				return c, nil
			}
		}
		return store.Connector{}, ErrNoChannel
	}
	if id == 0 {
		all, err := r.Store.ListConnectors(ctx)
		if err != nil {
			return store.Connector{}, err
		}
		for _, c := range all {
			if c.IsDefault && c.Type != "telegram" {
				id = c.ID
				break
			}
		}
		if id == 0 {
			return store.Connector{}, ErrNoChannel
		}
	}
	c, ok, err := r.Store.GetConnector(ctx, id)
	if err != nil {
		return c, err
	}
	if !ok || !c.Enabled || c.Type == "telegram" {
		return c, ErrNoChannel
	}
	return c, nil
}

// build decrypts the connector config and constructs the provider client.
func (r StoreResolver) build(ctx context.Context, c store.Connector) (connector.Connector, error) {
	cfg, err := r.Store.ConnectorConfig(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	return connector.Build(c.Type, cfg, r.HTTP)
}
