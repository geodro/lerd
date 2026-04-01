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
// On macOS all worker units (queue, schedule, stripe, reverb, horizon) are
// container-based — only native launchd services are excluded.
func isContainerUnit(unit string) bool {
	nativeUnits := map[string]bool{
		"lerd-dns":     true, // dnsmasq running natively via Homebrew
		"lerd-watcher": true,
		"lerd-ui":      true,
	}
	return !nativeUnits[unit]
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
// Container units use `podman logs -f`; native service units tail the launchd log file.
func logStreamCmd(ctx context.Context, unit string) *exec.Cmd {
	if isContainerUnit(unit) {
		// Wait up to 10s for the container to exist before streaming, so the
		// UI doesn't get an immediate "no such container" error when the log
		// panel opens right after a worker is started.
		bin := podman.PodmanBin()
		script := `for i in $(seq 1 20); do ` + bin + ` container exists ` + unit + ` 2>/dev/null && break; sleep 0.5; done; exec ` + bin + ` logs -f --tail 100 ` + unit
		return exec.CommandContext(ctx, "/bin/sh", "-c", script)
	}
	path := lerdLogPath(unit)
	script := `for i in $(seq 1 10); do [ -f "` + path + `" ] && break; sleep 0.5; done; exec tail -f -n 100 "` + path + `"`
	return exec.CommandContext(ctx, "/bin/sh", "-c", script)
}
