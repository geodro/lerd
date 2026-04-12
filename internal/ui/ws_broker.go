package ui

import (
	"net/http"
	"sync"

	"github.com/geodro/lerd/internal/eventbus"
)

// publishAfter wraps a mutating HTTP handler so that every successful
// invocation publishes the listed event kinds. The bus debounces bursty
// calls into a single websocket broadcast, so passing multiple kinds is
// cheap. Publish is called after the handler returns regardless of whether
// the response status was 2xx — lerd-ui actions either succeed and change
// state or fail and write an error body; in both cases the cached snapshot
// needs to be re-read, so the broadcast is harmless on failure.
func publishAfter(h http.HandlerFunc, kinds ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h(w, r)
		if r.Method == http.MethodGet || r.Method == http.MethodOptions {
			return
		}
		for _, k := range kinds {
			eventbus.Default.Publish(k)
		}
	}
}

// wsBroker is the in-process fan-out target for snapshot updates. It holds a
// set of per-connection channels that handleWS drains. The eventbus
// subscriber goroutine invalidates the snapshot cache, rebuilds the affected
// kinds, and then pushes the fresh bytes onto each broker channel.
//
// A second layer on top of eventbus is necessary because eventbus only
// carries "what kind changed"; the broker carries the rebuilt JSON bytes
// that the websocket handler writes to the socket.
type wsBroker struct {
	mu    sync.Mutex
	peers map[chan wsMessage]struct{}
}

// wsMessage is what the broker ships to each websocket writer goroutine.
// Kinds names which snapshots changed; Sites/Services/Status hold the fresh
// JSON bytes for only the kinds in Kinds.
type wsMessage struct {
	Kinds    []string
	Sites    []byte
	Services []byte
	Status   []byte
}

var broker = &wsBroker{peers: make(map[chan wsMessage]struct{})}

func (b *wsBroker) add() chan wsMessage {
	ch := make(chan wsMessage, 8)
	b.mu.Lock()
	b.peers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *wsBroker) remove(ch chan wsMessage) {
	b.mu.Lock()
	if _, ok := b.peers[ch]; ok {
		delete(b.peers, ch)
		close(ch)
	}
	b.mu.Unlock()
}

func (b *wsBroker) broadcast(msg wsMessage) {
	b.mu.Lock()
	var drop []chan wsMessage
	for ch := range b.peers {
		select {
		case ch <- msg:
		default:
			drop = append(drop, ch)
		}
	}
	for _, ch := range drop {
		delete(b.peers, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// runSnapshotInvalidator subscribes to the eventbus, invalidates the matching
// snapshot kinds, and ships the rebuilt bytes to the websocket broker.
func runSnapshotInvalidator() {
	sub := eventbus.Default.Subscribe()
	for evt := range sub.C {
		msg := wsMessage{Kinds: evt.Kinds}
		for _, k := range evt.Kinds {
			snapshots.Invalidate(k)
			switch k {
			case eventbus.KindSites:
				msg.Sites = snapshots.Sites()
			case eventbus.KindServices:
				msg.Services = snapshots.Services()
			case eventbus.KindStatus:
				msg.Status = snapshots.Status()
			}
		}
		broker.broadcast(msg)
	}
}
