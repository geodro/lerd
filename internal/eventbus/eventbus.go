// Package eventbus is an in-process pub/sub hub for UI state-change events.
//
// Mutations anywhere in the codebase (CLI commands, HTTP handlers, file
// watchers) call Publish to announce that a kind of state has changed. The
// hub debounces rapid publishes and broadcasts a single Event to every
// Subscriber. The ui package subscribes from the /api/ws handler so the
// browser can update without polling.
package eventbus

import (
	"sync"
	"time"
)

// Event kinds. Callers should use these constants rather than raw strings.
const (
	KindSites    = "sites"
	KindServices = "services"
	KindStatus   = "status"
)

// Event is broadcast to every Subscriber when a publish fires.
type Event struct {
	Kinds []string
	At    time.Time
}

// Subscriber receives events via C. Slow subscribers whose buffer fills up
// are dropped: the hub closes C and removes the subscriber.
type Subscriber struct {
	C      chan Event
	closed bool
}

// Hub coalesces publishes and fans them out to subscribers.
type Hub struct {
	mu       sync.Mutex
	subs     map[*Subscriber]struct{}
	dirty    map[string]struct{}
	timer    *time.Timer
	debounce time.Duration
	now      func() time.Time
}

// New returns a Hub with the default 150ms debounce window.
func New() *Hub {
	return &Hub{
		subs:     make(map[*Subscriber]struct{}),
		dirty:    make(map[string]struct{}),
		debounce: 150 * time.Millisecond,
		now:      time.Now,
	}
}

// Default is the process-wide Hub used by publishers that don't carry their
// own hub reference.
var Default = New()

// Subscribe registers a new subscriber with a buffered channel.
func (h *Hub) Subscribe() *Subscriber {
	s := &Subscriber{C: make(chan Event, 16)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

// Unsubscribe removes s and closes its channel if not already closed.
func (h *Hub) Unsubscribe(s *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; !ok {
		return
	}
	delete(h.subs, s)
	if !s.closed {
		s.closed = true
		close(s.C)
	}
}

// Publish marks kind dirty and (re)arms the debounce timer. Multiple
// publishes within the debounce window collapse into one broadcast.
func (h *Hub) Publish(kind string) {
	h.mu.Lock()
	h.dirty[kind] = struct{}{}
	if h.timer == nil {
		h.timer = time.AfterFunc(h.debounce, h.flush)
	} else {
		h.timer.Reset(h.debounce)
	}
	h.mu.Unlock()
}

func (h *Hub) flush() {
	h.mu.Lock()
	kinds := make([]string, 0, len(h.dirty))
	for k := range h.dirty {
		kinds = append(kinds, k)
	}
	h.dirty = make(map[string]struct{})
	h.timer = nil
	evt := Event{Kinds: kinds, At: h.now()}

	var drop []*Subscriber
	for s := range h.subs {
		if s.closed {
			drop = append(drop, s)
			continue
		}
		select {
		case s.C <- evt:
		default:
			drop = append(drop, s)
		}
	}
	for _, s := range drop {
		delete(h.subs, s)
		if !s.closed {
			s.closed = true
			close(s.C)
		}
	}
	h.mu.Unlock()
}

// SetDebounce changes the debounce window. Intended for tests.
func (h *Hub) SetDebounce(d time.Duration) {
	h.mu.Lock()
	h.debounce = d
	h.mu.Unlock()
}
