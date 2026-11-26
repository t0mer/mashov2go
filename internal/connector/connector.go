// Package connector delivers messages to family members over WhatsApp
// (Green-API) and Telegram. Each connector wraps one provider account; the
// outbox worker decides retries from the *SendError classification.
package connector

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Message is one outgoing message. Text is always set and is used as the plain
// fallback; HTML, when set, is the richer body for providers that support it.
type Message struct {
	Text    string
	HTML    string
	EventID string
}

// Status is the health of a connector as reported by Health.
type Status string

const (
	StatusOK              Status = "ok"
	StatusDown            Status = "down"
	StatusPairingRequired Status = "pairing_required"
)

// Connector sends messages through one provider account.
type Connector interface {
	// Type returns "greenapi" or "telegram" (matches connectors.type).
	Type() string
	// SendTo delivers m to address: a WhatsApp phone in E.164 digits, or a Telegram chat id.
	SendTo(ctx context.Context, address string, m Message) error
	// Health reports whether the provider account can currently send.
	Health(ctx context.Context) Status
	Close() error
}

// SendError classifies a failed send for the outbox. Permanent errors are not
// retried (bad number, chat not found, bad credentials). RetryAfter, when set,
// is the provider's own wait hint and overrides the outbox backoff.
type SendError struct {
	Permanent  bool
	RetryAfter time.Duration
	Err        error
}

func (e *SendError) Error() string { return e.Err.Error() }
func (e *SendError) Unwrap() error { return e.Err }

// IsPermanent reports whether err should not be retried.
func IsPermanent(err error) bool {
	var se *SendError
	return errors.As(err, &se) && se.Permanent
}

// RetryAfterOf returns the provider's wait hint in err, or 0.
func RetryAfterOf(err error) time.Duration {
	var se *SendError
	if errors.As(err, &se) {
		return se.RetryAfter
	}
	return 0
}

func transient(format string, args ...any) error {
	return &SendError{Err: fmt.Errorf(format, args...)}
}

func permanent(format string, args ...any) error {
	return &SendError{Permanent: true, Err: fmt.Errorf(format, args...)}
}
