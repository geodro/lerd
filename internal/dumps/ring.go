package dumps

import "sync"

// DefaultCapacity is the maximum number of events the ring keeps before it
// overwrites the oldest entry. Sized to match the web LogViewer's 500-line
// cap so the in-memory budget for either feed is similar.
const DefaultCapacity = 500

// Ring is a fixed-size ring buffer of Events safe for concurrent use.
// Snapshots are taken under a read lock and returned in insertion order.
type Ring struct {
	mu   sync.RWMutex
	buf  []Event
	head int // next write index
	size int // populated entries, 0..cap
	cap  int
}

// NewRing returns a ring with the given capacity. Non-positive capacity is
// replaced with DefaultCapacity.
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Ring{buf: make([]Event, capacity), cap: capacity}
}

// Append stores e, evicting the oldest entry once the ring is full.
func (r *Ring) Append(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.head] = e
	r.head = (r.head + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

// Snapshot returns a copy of the ring contents in insertion order (oldest
// first). The returned slice is independent of the ring's backing array.
func (r *Ring) Snapshot() []Event {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Event, 0, r.size)
	if r.size < r.cap {
		out = append(out, r.buf[:r.size]...)
		return out
	}
	out = append(out, r.buf[r.head:]...)
	out = append(out, r.buf[:r.head]...)
	return out
}

// Len returns the number of populated entries.
func (r *Ring) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.size
}

// Cap returns the maximum number of entries the ring can hold.
func (r *Ring) Cap() int {
	return r.cap
}

// Clear empties the ring. Subsequent Snapshot() returns an empty slice.
func (r *Ring) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.head = 0
	r.size = 0
	for i := range r.buf {
		r.buf[i] = Event{}
	}
}

// FilterOpts narrows a Snapshot. Zero-value fields are ignored.
type FilterOpts struct {
	// Site exact-matches Ctx.Site when non-empty.
	Site string
	// Ctx exact-matches Ctx.Type ("fpm" or "cli") when non-empty.
	Ctx string
	// SinceID drops events whose ID is lexicographically <= SinceID.
	SinceID string
	// Limit caps the returned slice to the most recent N entries.
	// Zero or negative means no limit.
	Limit int
}

// Filter returns a Snapshot filtered by opts, preserving insertion order.
func (r *Ring) Filter(opts FilterOpts) []Event {
	snap := r.Snapshot()
	out := make([]Event, 0, len(snap))
	for _, e := range snap {
		if opts.Site != "" && e.Ctx.Site != opts.Site {
			continue
		}
		if opts.Ctx != "" && e.Ctx.Type != opts.Ctx {
			continue
		}
		if opts.SinceID != "" && e.ID <= opts.SinceID {
			continue
		}
		out = append(out, e)
	}
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[len(out)-opts.Limit:]
	}
	return out
}
