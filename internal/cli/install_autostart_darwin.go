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
// lerd-autostart calls `lerd start` at login, which sequences the container
// startup correctly (podman machine must be up before containers start).
// Container plists are written with RunAtLoad=false, so they depend on this.
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
