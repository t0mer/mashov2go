package outbox

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/t0mer/mashov2go/internal/connector"
	"github.com/t0mer/mashov2go/internal/crypto"
	"github.com/t0mer/mashov2go/internal/family"
	"github.com/t0mer/mashov2go/internal/store"
)

// fakeConn returns scripted errors in order; nil means success.
type fakeConn struct {
	script []error
	calls  int
}

func (f *fakeConn) Type() string { return "fake" }
func (f *fakeConn) SendTo(context.Context, string, connector.Message) error {
	f.calls++
	if len(f.script) == 0 {
		return nil
	}
	err := f.script[0]
	f.script = f.script[1:]
	return err
}
func (f *fakeConn) Health(context.Context) connector.Status { return connector.StatusOK }
func (f *fakeConn) Close() error                            { return nil }

// fakeResolver hands out one connector per channel and reports ErrNoChannel for
// a WhatsApp member without a phone.
type fakeResolver struct {
	conns map[string]*fakeConn
}

func (r fakeResolver) Resolve(_ context.Context, m family.Member, channel string) (connector.Connector, string, error) {
	if channel == family.ChannelWhatsApp && m.WhatsAppPhone == "" {
		return nil, "", ErrNoChannel
	}
	c, ok := r.conns[channel]
	if !ok {
		return nil, "", ErrNoChannel
	}
	return c, "addr", nil
}

type harness struct {
	t      *testing.T
	st     *store.Store
	now    time.Time
	w      *Worker
	member store.Delivery
}

func newHarness(t *testing.T, m family.Member, conns map[string]*fakeConn) *harness {
	t.Helper()
	ctx := context.Background()
	key := make([]byte, 32)
	sealer, err := crypto.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"), sealer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := &harness{t: t, st: st, now: time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)}
	m.Language, m.QuietStart, m.QuietEnd, m.PlanTime = "he", "22:30", "06:30", "19:30"
	mem, err := st.CreateMember(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertEvent(ctx, store.Event{Category: "homework", Kind: "new", DedupeKey: "k1", DetectedAt: h.now, OccurredAt: h.now, Source: "diff", Data: "{}"}); err != nil {
		t.Fatal(err)
	}
	ok, err := st.EnqueueDelivery(ctx, store.Delivery{EventID: 1, MemberID: mem.ID, Channel: m.Channel, Body: "hello", NextAt: h.now})
	if err != nil || !ok {
		t.Fatalf("enqueue: %v %v", ok, err)
	}
	h.w = &Worker{Store: st, Resolver: fakeResolver{conns: conns}, Now: func() time.Time { return h.now }}
	d, err := st.DueDeliveries(ctx, h.now, 10)
	if err != nil || len(d) != 1 {
		t.Fatalf("due: %v %v", d, err)
	}
	h.member = d[0]
	return h
}

func (h *harness) get() store.Delivery {
	h.t.Helper()
	d, err := h.st.GetDelivery(context.Background(), h.member.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return d
}

func (h *harness) runOnce() {
	h.t.Helper()
	if _, err := h.w.RunOnce(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

func TestBackoffSchedule(t *testing.T) {
	want := []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour, time.Hour, time.Hour}
	for i, w := range want {
		if got := Backoff(i + 1); got != w {
			t.Errorf("Backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestSendSucceeds(t *testing.T) {
	conn := &fakeConn{}
	h := newHarness(t, family.Member{Role: family.RoleChild, Name: "Noa", Channel: family.ChannelWhatsApp, WhatsAppPhone: "972502222222", Active: true}, map[string]*fakeConn{family.ChannelWhatsApp: conn})
	h.runOnce()
	if d := h.get(); d.Status != "sent" || d.Attempts != 1 || conn.calls != 1 {
		t.Fatalf("after send: %+v calls=%d", d, conn.calls)
	}
}

func TestTransientErrorRetriesWithBackoffThenFails(t *testing.T) {
	transient := &connector.SendError{Err: errors.New("HTTP 500")}
	conn := &fakeConn{script: []error{transient, transient, transient, transient, transient, transient}}
	h := newHarness(t, family.Member{Role: family.RoleChild, Name: "Noa", Channel: family.ChannelWhatsApp, WhatsAppPhone: "972502222222", Active: true}, map[string]*fakeConn{family.ChannelWhatsApp: conn})

	h.runOnce()
	d := h.get()
	if d.Status != "pending" || d.Attempts != 1 {
		t.Fatalf("after 1st failure: %+v", d)
	}
	if want := h.now.Add(30 * time.Second); !d.NextAt.Equal(want) {
		t.Fatalf("next_at = %v, want %v", d.NextAt, want)
	}
	// Not due yet: nothing is sent before next_at.
	h.runOnce()
	if conn.calls != 1 {
		t.Fatalf("retried before backoff elapsed: calls=%d", conn.calls)
	}
	for i := 0; i < 5; i++ {
		h.now = h.get().NextAt
		h.runOnce()
	}
	if d := h.get(); d.Status != "failed" || d.Attempts != MaxAttempts {
		t.Fatalf("after max attempts: %+v", d)
	}
}

func TestPermanentErrorFailsImmediately(t *testing.T) {
	conn := &fakeConn{script: []error{&connector.SendError{Permanent: true, Err: errors.New("HTTP 400")}}}
	h := newHarness(t, family.Member{Role: family.RoleChild, Name: "Noa", Channel: family.ChannelWhatsApp, WhatsAppPhone: "972502222222", Active: true}, map[string]*fakeConn{family.ChannelWhatsApp: conn})
	h.runOnce()
	if d := h.get(); d.Status != "failed" || d.Attempts != 1 {
		t.Fatalf("permanent error: %+v", d)
	}
}

func TestRetryAfterOverridesBackoff(t *testing.T) {
	conn := &fakeConn{script: []error{&connector.SendError{RetryAfter: 45 * time.Minute, Err: errors.New("429")}}}
	h := newHarness(t, family.Member{Role: family.RoleChild, Name: "Noa", Channel: family.ChannelWhatsApp, WhatsAppPhone: "972502222222", Active: true}, map[string]*fakeConn{family.ChannelWhatsApp: conn})
	h.runOnce()
	if want := h.now.Add(45 * time.Minute); !h.get().NextAt.Equal(want) {
		t.Fatalf("next_at = %v, want %v", h.get().NextAt, want)
	}
}

func TestFallbackUsedOnce(t *testing.T) {
	wa := &fakeConn{script: []error{&connector.SendError{Permanent: true, Err: errors.New("bad number")}}}
	tg := &fakeConn{script: []error{&connector.SendError{Permanent: true, Err: errors.New("chat not found")}}}
	h := newHarness(t, family.Member{Role: family.RoleChild, Name: "Noa", Channel: family.ChannelWhatsApp, WhatsAppPhone: "972502222222", FallbackChannel: family.ChannelTelegram, Active: true},
		map[string]*fakeConn{family.ChannelWhatsApp: wa, family.ChannelTelegram: tg})

	h.runOnce()
	d := h.get()
	if d.Channel != family.ChannelTelegram || d.Status != "pending" || d.Attempts != 0 {
		t.Fatalf("not switched to fallback: %+v", d)
	}
	h.runOnce()
	if d := h.get(); d.Status != "failed" || d.Channel != family.ChannelTelegram {
		t.Fatalf("fallback failure should end the delivery: %+v", d)
	}
	if wa.calls != 1 || tg.calls != 1 {
		t.Errorf("calls wa=%d tg=%d", wa.calls, tg.calls)
	}
}

func TestNoChannelIsSkipped(t *testing.T) {
	h := newHarness(t, family.Member{Role: family.RoleParent, Name: "Tomer", Channel: family.ChannelWhatsApp, Active: true}, map[string]*fakeConn{})
	h.runOnce()
	if d := h.get(); d.Status != "skipped_no_channel" {
		t.Fatalf("expected skipped_no_channel, got %+v", d)
	}
}

func TestInactiveMemberSkipped(t *testing.T) {
	conn := &fakeConn{}
	h := newHarness(t, family.Member{Role: family.RoleChild, Name: "Noa", Channel: family.ChannelWhatsApp, WhatsAppPhone: "972502222222", Active: false}, map[string]*fakeConn{family.ChannelWhatsApp: conn})
	h.runOnce()
	if d := h.get(); d.Status != "skipped_no_channel" || conn.calls != 0 {
		t.Fatalf("inactive member got a send: %+v calls=%d", d, conn.calls)
	}
}

func TestEnqueueDedupesPerEventAndMember(t *testing.T) {
	h := newHarness(t, family.Member{Role: family.RoleChild, Name: "Noa", Channel: family.ChannelWhatsApp, WhatsAppPhone: "972502222222", Active: true}, map[string]*fakeConn{family.ChannelWhatsApp: &fakeConn{}})
	ok, err := h.st.EnqueueDelivery(context.Background(), store.Delivery{EventID: h.member.EventID, MemberID: h.member.MemberID, Channel: "whatsapp", Body: "again"})
	if err != nil || ok {
		t.Fatalf("duplicate enqueue: ok=%v err=%v", ok, err)
	}
}
