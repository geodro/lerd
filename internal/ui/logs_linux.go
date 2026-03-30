//go:build linux

package ui

import (
	"context"
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
