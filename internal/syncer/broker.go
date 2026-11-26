package syncer

import (
	"sync"

	"github.com/t0mer/mashov2go/internal/store"
)

// Broker fans out new events to subscribers (used by the SSE endpoint in Phase 5).
// A slow subscriber drops events rather than blocking a sync.
type Broker struct {
	mu   sync.Mutex
	subs map[chan store.Event]struct{}
}

// NewBroker returns an empty broker.
func NewBroker() *Broker {
	return &Broker{subs: map[chan store.Event]struct{}{}}
}

// Subscribe returns a buffered channel and a cancel func that removes it.
func (b *Broker) Subscribe() (<-chan store.Event, func()) {
	ch := make(chan store.Event, 32)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
	}
}

// Publish delivers ev to every subscriber that has room.
func (b *Broker) Publish(ev store.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
