//go:build linux

package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
)

func serviceRecentLogs(unit string) string {
	cmd := exec.Command("journalctl", "--user", "-u", unit+".service", "-n", "20", "--no-pager", "--output=short")
	out, _ := cmd.CombinedOutput()
	return strings.TrimSpace(string(out))
}

func logStreamCmd(ctx context.Context, unit string) *exec.Cmd {
	return exec.CommandContext(ctx, "journalctl", "--user", "-u", unit, "-f", "--no-pager", "-n", "100", "--output=cat")
}

// isContainerUnit returns true on Linux — all lerd units run as Podman containers.
func isContainerUnit(_ string) bool { return true }

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

	cursor := r.Header.Get("Last-Event-ID")
	args := []string{"--user", "-u", unit, "-f", "--no-pager", "--output=json"}
	if cursor != "" {
		args = append(args, "--after-cursor="+cursor)
	} else {
		args = append(args, "-n", "100")
	}

	pr, pw := io.Pipe()
	cmd := exec.CommandContext(r.Context(), "journalctl", args...)
	cmd.Stdout = pw
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "data: error starting logs: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	go func() {
		cmd.Wait() //nolint:errcheck
		pw.Close()
	}()

	type journalEntry struct {
		Cursor  string          `json:"__CURSOR"`
		Message json.RawMessage `json:"MESSAGE"`
	}

	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry journalEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		var msg string
		if len(entry.Message) > 0 && entry.Message[0] == '"' {
			json.Unmarshal(entry.Message, &msg) //nolint:errcheck
		} else {
			// journalctl encodes binary messages as a JSON array of bytes
			var b []byte
			if json.Unmarshal(entry.Message, &b) == nil {
				msg = string(b)
			}
		}
		fmt.Fprintf(w, "id: %s\ndata: %s\n\n", entry.Cursor, msg)
		flusher.Flush()
	}
}
