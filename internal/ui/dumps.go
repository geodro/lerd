package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dumps"
	"github.com/geodro/lerd/internal/dumpsops"
)

// dumpsServer is the singleton dump receiver started by ui.Start. It's nil
// while the listener is unbound (port collision or the process is shutting
// down) — every handler tolerates the nil case so the UI keeps working.
var dumpsServer atomic.Pointer[dumps.Server]

// startDumpsServer binds the Unix-socket receiver and stores it in the
// global pointer. Errors are logged and swallowed: a bind failure
// shouldn't take the UI down. The receiver is always-on regardless of the
// Dumps.Enabled toggle because the toggle controls FPM volume mounts (the
// *senders*), while listening is essentially free and lets us pick up
// dumps the moment the user enables the bridge without restarting lerd-ui.
//
// We listen on a Unix socket under ~/.local/share/lerd/run/ (already
// covered by the %h:%h bind mount every FPM container ships with) so the
// receiver isn't reachable from anywhere outside the user's home — no LAN
// surface, no host firewall exposure, no IP allowlist to babysit.
func startDumpsServer() {
	if err := os.MkdirAll(filepath.Dir(config.DumpsSocketPath()), 0755); err != nil {
		fmt.Printf("[WARN] creating run dir for dumps socket: %v\n", err)
		return
	}
	srv, err := dumps.ListenOn(context.Background(), "unix", config.DumpsSocketPath())
	if err != nil {
		fmt.Printf("[WARN] dumps receiver: %v — `lerd dump tail` and the dashboard Dumps tab will be empty\n", err)
		return
	}
	dumpsServer.Store(srv)
	fmt.Printf("Lerd dumps receiver listening on unix:%s\n", srv.Addr())
}

// handleDumpsList returns a JSON array of buffered events. Supports
// ?site=<name>, ?ctx=fpm|cli, ?since=<id>, ?limit=N. Empty filters return
// the full ring in insertion order.
func handleDumpsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	srv := dumpsServer.Load()
	if srv == nil {
		writeJSON(w, []dumps.Event{})
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	out := srv.Filter(dumps.FilterOpts{
		Site:    q.Get("site"),
		Ctx:     q.Get("ctx"),
		SinceID: q.Get("since"),
		Limit:   limit,
	})
	writeJSON(w, out)
}

// handleDumpsStatus is the JSON-shaped sibling of `lerd dump status`. It
// reflects current state to anyone connected (CLI, MCP, web tab) without
// requiring them to reach into the config file or the receiver themselves.
func handleDumpsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, _ := config.LoadGlobal()
	srv := dumpsServer.Load()
	resp := struct {
		Enabled     bool   `json:"enabled"`
		Passthrough bool   `json:"passthrough"`
		Listening   bool   `json:"listening"`
		Addr        string `json:"addr"`
		Count       int    `json:"count"`
		Subscribers int    `json:"subscribers"`
		LastTS      string `json:"last_ts"`
	}{
		Enabled:     cfg != nil && cfg.IsDumpsEnabled(),
		Passthrough: cfg != nil && cfg.IsDumpsPassthrough(),
		Addr:        "unix:" + config.DumpsSocketPath(),
	}
	if srv != nil {
		resp.Listening = true
		// Keep the "unix:" prefix that resp.Addr was initialised with;
		// srv.Addr() returns just the path which reads as ambiguous.
		resp.Count = srv.Len()
		resp.Subscribers = srv.Subscribers()
		if snap := srv.Snapshot(); len(snap) > 0 {
			resp.LastTS = snap[len(snap)-1].TS
		}
	}
	writeJSON(w, resp)
}

// handleDumpsStream is a Server-Sent Events stream of new events. The client
// reconnects on its own when the connection drops, mirroring the existing
// log SSE pattern. Snapshot of buffered events is replayed up front so the
// browser tab loads with the recent history visible.
func handleDumpsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	srv := dumpsServer.Load()
	if srv == nil {
		// No receiver bound; keep the connection alive so the client retries
		// transparently once startDumpsServer succeeds (e.g. after a stale
		// Unix socket has been cleared and lerd-ui rebound).
		<-r.Context().Done()
		return
	}

	q := r.URL.Query()
	filt := dumps.FilterOpts{
		Site: q.Get("site"),
		Ctx:  q.Get("ctx"),
	}

	// Replay the ring up front so a reconnecting browser sees recent dumps
	// without a manual refresh. Honour SinceID if the EventSource sent
	// Last-Event-ID, so reconnections don't double-send.
	since := r.Header.Get("Last-Event-ID")
	if since == "" {
		since = q.Get("since")
	}
	filt.SinceID = since
	for _, ev := range srv.Filter(filt) {
		writeSSEEvent(w, flusher, ev)
	}

	ch, unsub := srv.Subscribe()
	defer unsub()

	// Heartbeat keeps NAT/proxy timeouts at bay even when no dumps are
	// arriving. Browsers ignore comment lines (`:`).
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if filt.Site != "" && ev.Ctx.Site != filt.Site {
				continue
			}
			if filt.Ctx != "" && ev.Ctx.Type != filt.Ctx {
				continue
			}
			writeSSEEvent(w, flusher, ev)
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, ev dumps.Event) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	// id: header lets the EventSource client supply Last-Event-ID on reconnect.
	if ev.ID != "" {
		fmt.Fprintf(w, "id: %s\n", ev.ID)
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
	flusher.Flush()
}

// handleDumpsClear empties the receiver's ring. Restricted to loopback so a
// LAN client can't wipe a developer's working buffer.
func handleDumpsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	srv := dumpsServer.Load()
	if srv != nil {
		srv.Clear()
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDumpsPassthrough flips Dumps.Passthrough by delegating to
// dumpsops.SetPassthrough. Loopback-only because this restarts every
// installed FPM container — same trust boundary as the toggle.
func handleDumpsPassthrough(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		Enable bool `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	res, err := dumpsops.SetPassthrough(req.Enable)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// handleDumpsToggle flips Dumps.Enabled by delegating to dumpsops.Apply,
// then returns the post-state JSON. Loopback-only so LAN clients can't
// toggle capture state without authorization.
func handleDumpsToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		Enable bool `json:"enable"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	res, err := dumpsops.Apply(req.Enable)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}
