package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/geodro/lerd/internal/eventbus"
	"github.com/geodro/lerd/internal/podman"
)

// visibleClients tracks how many browser tabs currently report as visible.
// When at least one tab is focused the cache polls more aggressively.
var visibleClients atomic.Int32

const (
	intervalFocused = 15 * time.Second
	intervalIdle    = 60 * time.Second
)

func noteVisibility(visible bool) {
	if visible {
		if visibleClients.Add(1) == 1 {
			podman.Cache.SetInterval(intervalFocused)
		}
	} else {
		if visibleClients.Add(-1) <= 0 {
			visibleClients.Store(0)
			podman.Cache.SetInterval(intervalIdle)
		}
	}
}

// handleWS upgrades the HTTP connection to a websocket and streams snapshot
// updates to the client. The initial frame carries a full snapshot of sites,
// services, and status so the browser can render immediately. Subsequent
// frames carry only the kinds that changed.
func handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := wsUpgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer ws.Close()

	ch := broker.add()
	defer broker.remove(ch)

	// Force-refresh the container cache and invalidate all snapshot kinds so
	// the first frame always reflects current container state, not a cached
	// value from before lerd start ran. PollNow blocks until the podman ps
	// completes, so the snapshot below is guaranteed to use fresh data even
	// when multiple connections arrive simultaneously.
	podman.Cache.PollNow()
	snapshots.Invalidate(eventbus.KindSites)
	snapshots.Invalidate(eventbus.KindServices)
	snapshots.Invalidate(eventbus.KindStatus)

	// Initial snapshot: assemble one JSON object containing all three kinds.
	initial := assembleSnapshot(snapshots.Sites(), snapshots.Services(), snapshots.Status(), []string{"snapshot"})
	if err := ws.WriteText(initial); err != nil {
		return
	}

	// connVisible tracks whether THIS connection is currently counted as
	// visible. Only the reader goroutine writes it; the deferred cleanup
	// reads it after the reader has exited (close(done) happens-before the
	// <-done branch), so there is no data race.
	connVisible := true
	noteVisibility(true)
	defer func() {
		if connVisible {
			noteVisibility(false)
		}
	}()

	// Reader goroutine: handle ping/close/visibility frames.
	// Visibility frames carry {"type":"visibility","visible":bool} and let
	// the server tune the container cache polling interval.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			op, payload, err := ws.ReadFrame()
			if err != nil {
				return
			}
			switch op {
			case wsOpPing:
				if err := ws.WritePong(payload); err != nil {
					return
				}
			case wsOpClose:
				_ = ws.WriteClose()
				return
			case wsOpText:
				var msg struct {
					Type    string `json:"type"`
					Visible bool   `json:"visible"`
				}
				if json.Unmarshal(payload, &msg) == nil && msg.Type == "visibility" && msg.Visible != connVisible {
					noteVisibility(msg.Visible)
					connVisible = msg.Visible
				}
			}
		}
	}()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			frame := assembleSnapshot(msg.Sites, msg.Services, msg.Status, msg.Kinds)
			if err := ws.WriteText(frame); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

// assembleSnapshot builds a JSON object of the form:
//
//	{"type":"<type>","sites":<sites>,"services":<services>,"status":<status>}
//
// Any nil slice is omitted so clients can treat missing keys as "unchanged".
func assembleSnapshot(sites, services, status []byte, kinds []string) []byte {
	var buf bytes.Buffer
	buf.WriteString(`{"type":"`)
	if len(kinds) == 1 {
		buf.WriteString(kinds[0])
	} else {
		buf.WriteString("snapshot")
	}
	buf.WriteByte('"')
	if len(sites) > 0 {
		buf.WriteString(`,"sites":`)
		buf.Write(sites)
	}
	if len(services) > 0 {
		buf.WriteString(`,"services":`)
		buf.Write(services)
	}
	if len(status) > 0 {
		buf.WriteString(`,"status":`)
		buf.Write(status)
	}
	buf.WriteByte('}')
	return buf.Bytes()
}
