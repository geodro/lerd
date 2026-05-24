package ui

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// debugAction maps the URL suffix to a concrete command + timeout. Centralised
// so every entry uses the same shape — output streamed to bytes.Buffer with a
// hard timeout, then returned as `{ok, output}` JSON.
type debugAction struct {
	// argv is the command + args. Run from os.Executable() (= the running lerd
	// binary) so the action mirrors what the CLI would do, no subprocess that
	// shells out separately.
	argv    []string
	// when useShell is true, argv[0] is interpreted by /bin/sh -c so multi-tool
	// pipelines (journalctl | tail) work. Default is direct exec.
	useShell bool
	timeout  time.Duration
}

// debugActions enumerates the read-only diagnostic commands the dashboard
// can fire. Keep this list small and curated: each entry must be safe to run
// repeatedly without prompting and without modifying state.
var debugActions = map[string]debugAction{
	"doctor":      {argv: []string{"doctor"}, timeout: 60 * time.Second},
	"dns-check":   {argv: []string{"dns:check"}, timeout: 30 * time.Second},
	"containers":  {argv: []string{"podman", "ps", "-a", "--format", "{{.Names}}\t{{.Status}}\t{{.Image}}"}, timeout: 10 * time.Second},
	"recent-logs": {argv: []string{"journalctl --user -u 'lerd-*' --since '15 min ago' --no-pager -n 200"}, useShell: true, timeout: 15 * time.Second},
	"about":       {argv: []string{"about"}, timeout: 5 * time.Second},
}

// handleDebugAction dispatches /api/debug/{action} requests. POST only; GET is
// rejected so an accidental browser navigation can't trigger a slow shellout.
func handleDebugAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/api/debug/")
	def, ok := debugActions[action]
	if !ok {
		http.NotFound(w, r)
		return
	}

	var cmd *exec.Cmd
	if def.useShell {
		// Shell form: argv[0] is the pipeline string.
		cmd = exec.Command("sh", "-c", def.argv[0])
	} else if def.argv[0] == "podman" || def.argv[0] == "journalctl" {
		// External binary — invoke directly.
		cmd = exec.Command(def.argv[0], def.argv[1:]...)
	} else {
		// Lerd subcommand — re-exec the running binary so the action stays
		// consistent with the CLI surface (same flags, same code paths).
		self, err := os.Executable()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "could not resolve lerd binary: " + err.Error()})
			return
		}
		cmd = exec.Command(self, def.argv...)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	// Hard timeout via timer so a hung subprocess can't pin the goroutine.
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()

	select {
	case err := <-done:
		out := strings.TrimRight(buf.String(), "\n")
		if err != nil {
			writeJSON(w, map[string]any{
				"ok":     false,
				"error":  err.Error(),
				"output": out,
			})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "output": out})
	case <-time.After(def.timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		writeJSON(w, map[string]any{
			"ok":     false,
			"error":  "timeout after " + def.timeout.String(),
			"output": buf.String(),
		})
	}
}
