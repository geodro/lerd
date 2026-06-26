//go:build darwin

package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/unitlog"
)

func lerdLogPath(unit string) string { return unitlog.LogPath(unit) }

// isContainerUnit returns true for units whose logs come from `podman logs`
// rather than the launchd log file.
func isContainerUnit(unit string) bool { return unitlog.IsContainerUnit(unit) }

func serviceRecentLogs(unit string) string {
	if isContainerUnit(unit) {
		out, err := podman.Cmd("logs", "--tail", "20", unit).CombinedOutput()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	path := lerdLogPath(unit)
	cmd := exec.Command("tail", "-n", "20", path)
	out, _ := cmd.CombinedOutput()
	return strings.TrimSpace(string(out))
}

// logStreamCmd returns a command that tails the launchd log file for native
// service units (dns, watcher, ui). Container units are streamed directly
// via `podman logs` in handleLogs and do not use this path.
func logStreamCmd(ctx context.Context, unit string) *exec.Cmd {
	path := lerdLogPath(unit)
	script := `for i in $(seq 1 10); do [ -f "` + path + `" ] && break; sleep 0.5; done; exec tail -f -n 100 "` + path + `"`
	return exec.CommandContext(ctx, "/bin/sh", "-c", script)
}

// streamUnitLogs streams logs for a unit as SSE.
// Container units (workers, PHP-FPM, services) use `podman logs -f`.
// Native service units (dns, watcher, ui) tail the launchd log file.
func streamUnitLogs(w http.ResponseWriter, r *http.Request, unit string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Flush an SSE comment so the response headers go out immediately and
	// the browser's EventSource fires onopen. Without this a silent unit
	// (schedule between cron ticks, reverb before any WebSocket client
	// connects) leaves the UI stuck on "connecting...".
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	if isContainerUnit(unit) {
		// Wait up to 10s for the container to exist (e.g. right after start).
		bin := podman.PodmanBin()
		tail := "100"
		if r.Header.Get("Last-Event-ID") != "" {
			tail = "0"
		}
		// Wrap r.Context() in a cancel so the worker-mode migration can
		// kill this stream pre-emptively. Otherwise its `podman logs -f`
		// child holds a gvproxy slot and races the migration's `podman
		// rm -f` against the same container.
		streamCtx, streamCancel := context.WithCancel(r.Context())
		defer streamCancel()
		if isFrameworkWorkerUnit(unit) {
			defer logStreams.Register(unit, streamCancel)()
		}
		script := `for i in $(seq 1 20); do ` + bin + ` container exists ` + unit + ` 2>/dev/null && break; sleep 0.5; done; exec ` + bin + ` logs -f --tail ` + tail + ` ` + unit
		cmd := exec.CommandContext(streamCtx, "/bin/sh", "-c", script)
		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(w, "data: error starting logs: %s\n\n", err.Error())
			flusher.Flush()
			return
		}
		go func() { cmd.Wait(); pw.Close() }() //nolint:errcheck
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
			flusher.Flush()
		}
		if cmd.Process != nil {
			cmd.Process.Kill() //nolint:errcheck
		}
		return
	}

	// Native service: tail the launchd log file.
	pr, pw := io.Pipe()
	cmd := logStreamCmd(r.Context(), unit)
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "data: error starting logs: %s\n\n", err.Error())
		flusher.Flush()
		return
	}
	go func() { cmd.Wait(); pw.Close() }() //nolint:errcheck
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
		flusher.Flush()
	}
}
