package dumps

import "sync"

// subBuffer is the per-subscriber channel depth. Slow consumers that don't
// drain in time miss events rather than block the publisher. Sized to absorb
// short bursts (request with several dumps in flight) but small enough that
// a stuck subscriber's memory cost is bounded.
const subBuffer = 64

// Hub fans Events out to subscribers. Publish is non-blocking; subscribers
// that don't drain fast enough drop events instead of stalling the listener.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewHub returns an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: map[chan Event]struct{}{}}
}

// Subscribe returns a buffered channel that receives every subsequently
// published event, plus an unsubscribe func that closes the channel and
// removes it from the hub. The unsubscribe is idempotent.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	closed := false
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if closed {
			return
		}
		closed = true
		delete(h.subs, ch)
		close(ch)
	}
}

// Publish delivers e to every active subscriber. Subscribers whose buffer
// is full miss the event; the publisher never blocks.
func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Count returns the number of active subscribers (for status/diagnostics).
func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
