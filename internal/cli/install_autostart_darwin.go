//go:build darwin

package cli

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/services"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
)

// installAutostart enables the lerd-autostart launchd service on macOS so that
// lerd starts automatically on every login. On macOS this is on by default
// (matching Herd's behaviour); on Linux it is opt-in via `lerd autostart enable`.
func installAutostart() {
	content, err := lerdSystemd.GetUnit("lerd-autostart")
	if err != nil {
		fmt.Fprintf(os.Stderr, "  WARN: autostart unit: %v\n", err)
		return
	}
	if err := services.Mgr.WriteServiceUnit("lerd-autostart", content); err != nil {
		fmt.Fprintf(os.Stderr, "  WARN: writing autostart service: %v\n", err)
		return
	}
	if err := services.Mgr.Enable("lerd-autostart"); err != nil {
		fmt.Fprintf(os.Stderr, "  WARN: enabling autostart: %v\n", err)
	}
}
