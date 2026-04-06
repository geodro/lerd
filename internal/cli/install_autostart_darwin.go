//go:build darwin

package cli

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/services"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
)

// installAutostart enables the lerd-autostart and lerd-tray launchd services on
// macOS so that lerd and its tray applet start automatically on every login.
// On macOS this is on by default (matching Herd's behaviour); on Linux it is
// opt-in via `lerd autostart enable`.
func installAutostart() {
	for _, unit := range []string{"lerd-autostart", "lerd-tray"} {
		content, err := lerdSystemd.GetUnit(unit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  WARN: %s unit: %v\n", unit, err)
			continue
		}
		if err := services.Mgr.WriteServiceUnit(unit, content); err != nil {
			fmt.Fprintf(os.Stderr, "  WARN: writing %s service: %v\n", unit, err)
			continue
		}
		if err := services.Mgr.Enable(unit); err != nil {
			fmt.Fprintf(os.Stderr, "  WARN: enabling %s: %v\n", unit, err)
		}
	}
}
