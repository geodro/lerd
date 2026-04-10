package systemd

import (
	"path/filepath"
	"sort"

	"github.com/geodro/lerd/internal/config"
)

// AutostartUserUnits returns the lerd-* systemd user units (NOT podman
// quadlets) that participate in "autostart at login": lerd-ui,
// lerd-watcher, lerd-tray, and every per-site worker/queue/schedule/
// horizon/reverb/stripe service file currently present in the user's
// systemd/user/ directory. These can be enabled/disabled with
// `systemctl --user enable/disable` because they are real on-disk unit
// files (not generator output).
//
// Container quadlets are NOT in this list because their auto-start
// behaviour is controlled by stripping the [Install] section from the
// .container file (see podman.StripInstallSection), not by systemctl
// enable/disable. The autostart toggle handles quadlets via the global
// config flag and a quadlet rewrite pass.
func AutostartUserUnits() []string {
	seen := map[string]struct{}{
		"lerd-ui.service":      {},
		"lerd-watcher.service": {},
		"lerd-tray.service":    {},
	}
	if entries, err := filepath.Glob(filepath.Join(config.SystemdUserDir(), "lerd-*.service")); err == nil {
		for _, f := range entries {
			name := filepath.Base(f)
			seen[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// IsAutostartEnabled reports whether lerd is configured to come up at
// login. This is the inverse of cfg.Autostart.Disabled — the config flag
// is the canonical source of truth, not the live `systemctl is-enabled`
// state of any individual unit, because (a) container quadlets always
// report "generated" regardless of the wants symlink and (b) the flag
// must be readable even when no units exist on disk yet (fresh install).
func IsAutostartEnabled() bool {
	cfg, err := config.LoadGlobal()
	if err != nil || cfg == nil {
		// Missing config = treat as enabled, matching the historical
		// behaviour for every install that pre-dates this flag.
		return true
	}
	return !cfg.Autostart.Disabled
}
