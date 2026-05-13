//go:build darwin

package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// workerLogCmd returns the command that tails a worker's output on macOS.
// In exec mode workers run as launchd service units — their stdout/stderr go
// to ~/Library/Logs/lerd/<unit>.log, so we tail that file. In container mode
// workers are detached podman containers, so we use `podman logs -f`.
func workerLogCmd(ctx context.Context, unit string) *exec.Cmd {
	if isExecModeUnit(unit) {
		home, _ := os.UserHomeDir()
		logPath := filepath.Join(home, "Library", "Logs", "lerd", unit+".log")
		script := `for i in $(seq 1 20); do [ -f "` + logPath + `" ] && break; sleep 0.25; done; exec tail -f -n 200 "` + logPath + `"`
		return exec.CommandContext(ctx, "/bin/sh", "-c", script)
	}
	bin := podman.PodmanBin()
	script := `for i in $(seq 1 20); do ` + bin + ` container exists ` + unit +
		` 2>/dev/null && break; sleep 0.25; done; exec ` + bin + ` logs -f --tail 200 ` + unit
	return exec.CommandContext(ctx, "/bin/sh", "-c", script)
}

// isExecModeUnit returns true when a unit should be treated as a launchd service
// unit (exec mode) rather than a detached podman container.
//
// Detection order:
//  1. If the plist exists: trust its RunAtLoad flag — service units have it,
//     container units don't. This handles in-flight mode changes correctly.
//  2. No plist (unit never started, or migration cleaned it up): fall back to
//     the global config. Only apply to known framework worker prefixes so that
//     infrastructure containers (FPM, nginx, services) are never misclassified.
func isExecModeUnit(unit string) bool {
	home, _ := os.UserHomeDir()
	plist, err := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", unit+".plist"))
	if err == nil {
		return strings.Contains(string(plist), "<key>RunAtLoad</key>")
	}
	if !isFrameworkWorkerUnit(unit) {
		return false
	}
	cfg, _ := config.LoadGlobal()
	return cfg != nil && cfg.WorkerExecMode() != config.WorkerExecModeContainer
}

// isFrameworkWorkerUnit reports whether unit looks like a built-in framework
// worker (queue, schedule, horizon, reverb). Used as a guard so that the
// config-based exec-mode fallback never misclassifies infrastructure containers.
func isFrameworkWorkerUnit(unit string) bool {
	for _, prefix := range []string{"lerd-queue-", "lerd-schedule-", "lerd-horizon-", "lerd-reverb-"} {
		if strings.HasPrefix(unit, prefix) {
			return true
		}
	}
	return false
}
