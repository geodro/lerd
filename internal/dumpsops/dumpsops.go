// Package dumpsops contains the shared business logic for toggling the lerd
// dump bridge: persist config, write/remove the bridge assets, regenerate
// every installed PHP version's FPM quadlet, and restart only the units that
// actually changed. The CLI, UI server, and MCP all call into here so the
// three surfaces stay in lockstep on state transitions and ordering.
package dumpsops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// Result describes the outcome of Apply so callers can render their own
// user-facing message without inspecting state again.
type Result struct {
	Enabled    bool     // post-apply state
	NoChange   bool     // requested state already matched; no quadlets touched
	Changed    []string // FPM unit names whose quadlet content changed
	Restarted  []string // units that successfully restarted after a change
	RestartErr error    // first restart error encountered, if any
}

// Apply flips the Dumps.Enabled flag to enabled, then ensures the host-side
// bridge assets and FPM container quadlets reflect the requested state.
// Idempotent: a second call with the same value is a no-op (Result.NoChange).
//
// Apply intentionally does NOT start a TCP receiver on its own. lerd-ui owns
// the receiver lifecycle and reacts to config changes via OnConfigChange so
// the receiver moves in lockstep with the toggle without coupling the
// business logic here to the UI process.
func Apply(enabled bool) (Result, error) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return Result{}, fmt.Errorf("loading config: %w", err)
	}

	if cfg.IsDumpsEnabled() == enabled {
		return Result{Enabled: enabled, NoChange: true}, nil
	}

	cfg.SetDumpsEnabled(enabled)
	if err := config.SaveGlobal(cfg); err != nil {
		return Result{Enabled: enabled}, fmt.Errorf("saving config: %w", err)
	}

	if enabled {
		if err := podman.WriteDumpBridgeAssets(); err != nil {
			return Result{Enabled: true}, fmt.Errorf("writing dump assets: %w", err)
		}
	} else {
		// Best-effort: if removal fails (e.g. the file vanished out from under
		// us) we still want to rewrite the quadlets so the Volume= lines drop.
		_ = podman.RemoveDumpAssets()
	}

	changed, err := rewriteFPMQuadlets(enabled)
	if err != nil {
		return Result{Enabled: enabled}, err
	}
	res := Result{Enabled: enabled, Changed: changed}

	for _, unit := range changed {
		if rerr := podman.RestartUnit(unit); rerr != nil {
			if res.RestartErr == nil {
				res.RestartErr = rerr
			}
			continue
		}
		res.Restarted = append(res.Restarted, unit)
	}
	return res, nil
}

// rewriteFPMQuadlets regenerates the per-version lerd-php{ver}-fpm.container
// files using the current Dumps state and reports the unit names whose
// content actually changed (so callers can restart only those).
func rewriteFPMQuadlets(dumpsEnabled bool) ([]string, error) {
	versions, err := listInstalledPHPVersions()
	if err != nil {
		return nil, fmt.Errorf("listing PHP versions: %w", err)
	}
	var changed []string
	for _, v := range versions {
		short := strings.ReplaceAll(v, ".", "")
		unit := "lerd-php" + short + "-fpm"
		tmpl, err := podman.GetQuadletTemplate("lerd-php-fpm.container.tmpl")
		if err != nil {
			return nil, err
		}
		content := strings.ReplaceAll(tmpl, "{{.Version}}", v)
		content = strings.ReplaceAll(content, "{{.VersionShort}}", short)
		content = strings.ReplaceAll(content, "{{.XdebugIniPath}}", config.PHPConfFile(v))
		content = strings.ReplaceAll(content, "{{.UserIniPath}}", config.PHPUserIniFile(v))
		content = podman.ApplyDumpVolumes(content, dumpsEnabled)
		content = podman.InjectExtraVolumes(content, podman.ExtraVolumePaths())

		didChange, werr := podman.WriteQuadletDiff(unit, content)
		if werr != nil {
			return changed, fmt.Errorf("writing %s quadlet: %w", unit, werr)
		}
		if didChange {
			changed = append(changed, unit)
		}
	}
	if len(changed) > 0 {
		if err := podman.DaemonReloadFn(); err != nil {
			return changed, fmt.Errorf("daemon-reload: %w", err)
		}
	}
	return changed, nil
}

// listInstalledPHPVersions returns versions that have an FPM quadlet on disk.
// Mirrors the unexported helper of the same name in internal/podman so the
// CLI/UI can reach it without tripping the import cycle that would arise if
// dumpsops imported the unexported version.
func listInstalledPHPVersions() ([]string, error) {
	entries, err := os.ReadDir(config.QuadletDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var versions []string
	for _, e := range entries {
		name := filepath.Base(e.Name())
		if !strings.HasPrefix(name, "lerd-php") || !strings.HasSuffix(name, "-fpm.container") {
			continue
		}
		short := strings.TrimSuffix(strings.TrimPrefix(name, "lerd-php"), "-fpm.container")
		if len(short) < 2 {
			continue
		}
		versions = append(versions, string(short[0])+"."+short[1:])
	}
	return versions, nil
}
