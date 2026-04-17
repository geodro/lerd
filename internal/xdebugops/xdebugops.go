// Package xdebugops contains the shared business logic for toggling Xdebug
// on a PHP version: mode validation, config persistence, ini write, FPM
// quadlet update, and unit restart. The CLI, UI, and MCP all call into here
// so the three surfaces stay in lockstep on state transitions and ordering.
package xdebugops

import (
	"fmt"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// Result describes the outcome of Apply so callers can render their own
// user-facing message without inspecting the config again.
type Result struct {
	Version   string
	Mode      string // canonical mode after Apply; "" means xdebug disabled
	Enabled   bool   // convenience: Mode != ""
	NoChange  bool   // true when the requested state already matched; no restart was attempted
	Restarted bool   // true when the FPM unit restart succeeded
	// RestartErr is set when the FPM unit restart failed. Non-fatal: config and
	// ini are already persisted, the caller just needs to surface a hint so
	// the user can restart the unit manually.
	RestartErr error
}

// Apply toggles Xdebug for version. An empty mode disables xdebug; a non-empty
// mode is validated via podman.NormaliseXdebugMode and enables xdebug with
// that canonical value. Apply is idempotent: passing the current mode is a
// no-op that returns NoChange=true without touching the FPM container.
func Apply(version, rawMode string) (Result, error) {
	targetMode := ""
	if rawMode != "" {
		m, err := podman.NormaliseXdebugMode(rawMode)
		if err != nil {
			return Result{Version: version}, err
		}
		targetMode = m
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return Result{Version: version}, fmt.Errorf("loading config: %w", err)
	}

	if cfg.GetXdebugMode(version) == targetMode {
		return Result{
			Version:  version,
			Mode:     targetMode,
			Enabled:  targetMode != "",
			NoChange: true,
		}, nil
	}

	cfg.SetXdebugMode(version, targetMode)
	if err := config.SaveGlobal(cfg); err != nil {
		return Result{Version: version}, fmt.Errorf("saving config: %w", err)
	}

	if err := podman.WriteXdebugIni(version, targetMode); err != nil {
		return Result{Version: version}, fmt.Errorf("writing xdebug ini: %w", err)
	}

	if err := podman.WriteFPMQuadlet(version); err != nil {
		return Result{Version: version}, fmt.Errorf("writing FPM quadlet: %w", err)
	}

	res := Result{
		Version: version,
		Mode:    targetMode,
		Enabled: targetMode != "",
	}
	unit := "lerd-php" + strings.ReplaceAll(version, ".", "") + "-fpm"
	if err := podman.RestartUnit(unit); err != nil {
		res.RestartErr = err
		return res, nil
	}
	res.Restarted = true
	return res, nil
}

// FPMUnit returns the systemd unit name for a PHP version's FPM container.
// Exposed so callers can print consistent "Run: systemctl --user restart ..."
// hints when Apply's RestartErr is non-nil.
func FPMUnit(version string) string {
	return "lerd-php" + strings.ReplaceAll(version, ".", "") + "-fpm"
}
