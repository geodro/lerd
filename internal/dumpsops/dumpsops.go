// Package dumpsops contains the shared business logic for toggling the lerd
// dump bridge. Implementation is restart-free: the bridge PHP file and its
// conf.d ini are always volume-mounted into every FPM container, and the
// active/inactive state is signalled by a sentinel file inside the same
// mount. Toggling is a single filesystem touch and applies on the next
// PHP request without any container or worker disruption.
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
	Enabled  bool // post-apply state
	NoChange bool // requested state already matched; no FS changes
}

// Apply flips Dumps.Enabled to the requested state. Persists the config
// flag, ensures the bridge assets exist on disk, and touches/removes the
// runtime sentinel that controls bridge behaviour. No FPM containers are
// restarted because the assets are always volume-mounted; the bridge
// reads the sentinel on each request.
//
// Idempotent: a second call with the same value returns NoChange=true
// without touching the filesystem.
//
// Ordering: the sentinel is written before the config is persisted on
// enable, and removed after the config is persisted on disable. That way
// the two state surfaces (config.yaml + sentinel) only ever disagree in
// the safe direction — the bridge no-ops when the config says enabled,
// rather than capturing when the config says disabled.
func Apply(enabled bool) (Result, error) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return Result{}, fmt.Errorf("loading config: %w", err)
	}

	if cfg.IsDumpsEnabled() == enabled {
		return Result{Enabled: enabled, NoChange: true}, nil
	}

	// Always make sure the bridge files exist on disk. Even when the user
	// is turning the bridge OFF, lerd-ui must keep them there because the
	// FPM quadlet has them as bind-mount sources — removing them would
	// make podman auto-create directories at those paths on the next FPM
	// start.
	if err := podman.WriteDumpBridgeAssets(); err != nil {
		return Result{Enabled: enabled}, fmt.Errorf("writing dump assets: %w", err)
	}

	if enabled {
		if err := podman.SetDumpsBridgeFlag(true); err != nil {
			return Result{Enabled: false}, err
		}
		cfg.SetDumpsEnabled(true)
		if err := config.SaveGlobal(cfg); err != nil {
			// Roll back the sentinel so config + runtime stay aligned.
			_ = podman.SetDumpsBridgeFlag(false)
			return Result{Enabled: false}, fmt.Errorf("saving config: %w", err)
		}
		return Result{Enabled: true}, nil
	}

	cfg.SetDumpsEnabled(false)
	if err := config.SaveGlobal(cfg); err != nil {
		return Result{Enabled: true}, fmt.Errorf("saving config: %w", err)
	}
	if err := podman.SetDumpsBridgeFlag(false); err != nil {
		return Result{Enabled: false}, err
	}
	return Result{Enabled: false}, nil
}

// PassthroughResult describes the outcome of SetPassthrough so callers
// can render which FPM units were restarted to apply the change.
type PassthroughResult struct {
	Passthrough bool
	NoChange    bool
	Restarted   []string
	RestartErr  error
}

// SetPassthrough flips the passthrough flag and rewrites the conf.d ini
// with the new value. PHP reads ini directives at FPM startup, so every
// installed FPM unit is restarted to apply the change. This is the only
// dumps op that ever restarts an FPM container; the Enable/Disable path
// stays restart-free.
func SetPassthrough(enabled bool) (PassthroughResult, error) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return PassthroughResult{}, fmt.Errorf("loading config: %w", err)
	}
	if cfg.IsDumpsPassthrough() == enabled {
		return PassthroughResult{Passthrough: enabled, NoChange: true}, nil
	}
	cfg.SetDumpsPassthrough(enabled)
	if err := config.SaveGlobal(cfg); err != nil {
		return PassthroughResult{Passthrough: !enabled}, fmt.Errorf("saving config: %w", err)
	}
	// Rewrite the conf.d ini so the new lerd.dump_passthrough value lands
	// on disk. The bridge file itself is unchanged; only the ini differs.
	if err := podman.WriteDumpBridgeAssets(); err != nil {
		return PassthroughResult{Passthrough: enabled}, fmt.Errorf("rewriting bridge ini: %w", err)
	}
	res := PassthroughResult{Passthrough: enabled}
	for _, unit := range installedFPMUnits() {
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

func installedFPMUnits() []string {
	entries, err := os.ReadDir(config.QuadletDir())
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := filepath.Base(e.Name())
		if !strings.HasPrefix(name, "lerd-php") || !strings.HasSuffix(name, "-fpm.container") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".container"))
	}
	return out
}
