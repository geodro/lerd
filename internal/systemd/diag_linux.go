//go:build linux

package systemd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
)

// unitFailureDetail turns systemd's opaque job result (usually the bare word
// "failed") into something a user can act on. It pulls the service Result
// property and the last few journal lines for the unit, so a quadlet that
// dies on "container name is already in use" or an OCI runtime error reports
// the real reason instead of just "failed: failed". Best-effort: any lookup
// that errors is skipped and an empty string means "no detail available".
func unitFailureDetail(conn *dbus.Conn, name string) string {
	unit := withServiceSuffix(name)
	return formatUnitFailureDetail(serviceResult(conn, unit), journalTail(unit, 8))
}

// formatUnitFailureDetail assembles the trailing detail appended to a unit-op
// error from the service-result summary and the journal tail. Pure (no DBus or
// exec) so the formatting is unit-testable; returns "" when neither is present.
func formatUnitFailureDetail(header, logs string) string {
	if header == "" && logs == "" {
		return ""
	}
	var b strings.Builder
	if header != "" {
		fmt.Fprintf(&b, " (%s)", header)
	}
	if logs != "" {
		b.WriteString("\n")
		b.WriteString(logs)
	}
	return b.String()
}

// serviceResult reads the Service-type Result property (e.g. "exit-code",
// "timeout", "resources", "oom-kill") plus the main process exit status when
// present, summarising why the unit's process failed. Empty on any error or
// when the result is the unremarkable "success".
func serviceResult(conn *dbus.Conn, unit string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	props, err := conn.GetUnitTypePropertiesContext(ctx, unit, "Service")
	if err != nil {
		return ""
	}
	result, _ := props["Result"].(string)
	if result == "" || result == "success" {
		return ""
	}
	out := "result: " + result
	if code, ok := props["ExecMainStatus"].(int32); ok && code != 0 {
		out += fmt.Sprintf(", exit status %d", code)
	}
	return out
}

// journalTail returns the last n journal lines for the unit's most recent
// invocation, message-only, trimmed and indented for display. Empty if
// journalctl is unavailable or produced nothing.
func journalTail(unit string, n int) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "journalctl", "--user",
		"-u", unit, "-n", fmt.Sprintf("%d", n), "--no-pager", "-o", "cat")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var lines []string
	for _, ln := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || ln == "-- No entries --" {
			continue
		}
		lines = append(lines, "    "+ln)
	}
	return strings.Join(lines, "\n")
}
