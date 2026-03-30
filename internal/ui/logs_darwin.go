//go:build darwin

package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/podman"
)

func lerdLogPath(unit string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "lerd", unit+".log")
}

// isContainerUnit returns true for units that run as detached containers
// (podman run -d). Their logs come from `podman logs`, not the launchd log file.
// Service units (queue, schedule, stripe, reverb, horizon, workers) run
// `podman exec` and write to the launchd log file.
func isContainerUnit(unit string) bool {
	// Native launchd services (not podman containers) — logs are in the launchd log file.
	nativeUnits := map[string]bool{
		"lerd-dns":     true, // dnsmasq running natively via Homebrew
		"lerd-watcher": true,
		"lerd-ui":      true,
	}
	if nativeUnits[unit] {
		return false
	}
	// Exec-based worker units write to the launchd log file.
	execPrefixes := []string{
		"lerd-queue-", "lerd-schedule-", "lerd-stripe-",
		"lerd-reverb-", "lerd-horizon-",
	}
	for _, p := range execPrefixes {
		if strings.HasPrefix(unit, p) {
			return false
		}
	}
	return true
}

func serviceRecentLogs(unit string) string {
	if isContainerUnit(unit) {
		out, err := exec.Command(podman.PodmanBin(), "logs", "--tail", "20", unit).CombinedOutput()
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

// logStreamCmd returns a command that streams logs for the unit.
// Container units use `podman logs -f`; exec-based service units tail the launchd log file.
func logStreamCmd(ctx context.Context, unit string) *exec.Cmd {
	if isContainerUnit(unit) {
		return exec.CommandContext(ctx, podman.PodmanBin(), "logs", "-f", "--tail", "100", unit)
	}
	path := lerdLogPath(unit)
	script := `for i in $(seq 1 10); do [ -f "` + path + `" ] && break; sleep 0.5; done; exec tail -f -n 100 "` + path + `"`
	return exec.CommandContext(ctx, "/bin/sh", "-c", script)
}
