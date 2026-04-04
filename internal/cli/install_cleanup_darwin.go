//go:build darwin

package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

const lerdCleanupScript = `#!/bin/sh
# lerd-cleanup — standalone Lerd uninstaller for macOS.
# Run this if you already removed the lerd binary via brew uninstall lerd
# and need to clean up services, launch agents, and DNS config.

set -e

echo "==> Lerd cleanup"

# ── Stop and bootout all lerd launchd services ──────────────────────────────
DOMAIN="gui/$(id -u)"
for plist in "$HOME/Library/LaunchAgents/lerd-"*.plist; do
  [ -f "$plist" ] || continue
  label=$(defaults read "$plist" Label 2>/dev/null)
  [ -n "$label" ] || continue
  echo "  --> Stopping $label"
  launchctl bootout "$DOMAIN/$label" 2>/dev/null || true
done

# ── Remove lerd launch agent plists ─────────────────────────────────────────
echo "  --> Removing launch agents"
rm -f "$HOME/Library/LaunchAgents/lerd-"*.plist

# ── Stop and remove lerd podman containers ───────────────────────────────────
if command -v podman >/dev/null 2>&1; then
  for ctr in $(podman ps -a --format '{{.Names}}' 2>/dev/null | grep '^lerd-' || true); do
    echo "  --> Removing container $ctr"
    podman stop "$ctr" 2>/dev/null || true
    podman rm -f "$ctr" 2>/dev/null || true
  done
  echo "  --> Removing lerd podman network"
  podman network rm lerd 2>/dev/null || true
fi

# ── Remove /etc/resolver entry ───────────────────────────────────────────────
if [ -f /etc/resolver/test ]; then
  echo "  --> Removing /etc/resolver/test (requires sudo)"
  sudo rm -f /etc/resolver/test
fi

# ── Remove log files ─────────────────────────────────────────────────────────
rm -rf "$HOME/Library/Logs/lerd"

echo ""
printf "  Remove config and data (~/.config/lerd, ~/.local/share/lerd)? [y/N] "
read -r ans
case "$ans" in
  [Yy]|[Yy][Ee][Ss])
    rm -rf "$HOME/.config/lerd" "$HOME/.local/share/lerd"
    echo "  --> Config and data removed."
    ;;
  *)
    echo "  --> Config and data kept."
    ;;
esac

echo ""
echo "Lerd cleanup complete."
echo "Run 'brew uninstall lerd' if you haven't already."
`

// installCleanupScript writes a standalone shell uninstaller to ~/.local/bin/lerd-cleanup.
// This lets macOS users clean up lerd's launchd agents, containers, and DNS config
// even if they already removed the lerd binary via `brew uninstall lerd`.
func installCleanupScript() {
	binDir := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		fmt.Printf("    WARN: could not create %s: %v\n", binDir, err)
		return
	}
	dest := filepath.Join(binDir, "lerd-cleanup")
	if err := os.WriteFile(dest, []byte(lerdCleanupScript), 0755); err != nil {
		fmt.Printf("    WARN: could not write lerd-cleanup script: %v\n", err)
	}
}
