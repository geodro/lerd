//go:build darwin

package tui

import (
	"context"
	"os/exec"

	"github.com/geodro/lerd/internal/podman"
)

// workerLogCmd returns the command that tails a worker's output on macOS.
// Workers on macOS run as detached podman containers (not systemd units),
// so their logs come from `podman logs -f`, matching what lerd-ui's
// logs_darwin.go does. The short-retry wrapper exists because workers
// started from the TUI may not have fully materialised yet when `l` fires.
func workerLogCmd(ctx context.Context, container string) *exec.Cmd {
	bin := podman.PodmanBin()
	script := `for i in $(seq 1 20); do ` + bin + ` container exists ` + container +
		` 2>/dev/null && break; sleep 0.25; done; exec ` + bin + ` logs -f --tail 200 ` + container
	return exec.CommandContext(ctx, "/bin/sh", "-c", script)
}
