//go:build linux

package tui

import (
	"context"
	"os/exec"
)

// workerLogCmd returns the command that tails a worker's output on Linux.
// Workers run as systemd user units, so their stdout/stderr are in the
// user journal — `journalctl --user` is the right source, matching what
// lerd-ui's logs_linux.go does.
func workerLogCmd(ctx context.Context, unit string) *exec.Cmd {
	return exec.CommandContext(ctx, "journalctl", "--user", "-u", unit,
		"-f", "--no-pager", "-n", "200", "--output=cat")
}
