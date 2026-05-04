package serviceops

import (
	"fmt"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// Seams for ReinstallService composition. The defaults wire to the real
// install + reprovision functions; tests substitute fakes.
var (
	reinstallInstallFn = realReinstallInstall
	reinstallReprovFn  = ReprovisionLinkedSites
)

// ReinstallService stops, removes, and reinstalls a service, optionally wiping
// its data and reprovisioning per-site state (databases, buckets) on the
// fresh service.
//
// Reinstall requires that the service is currently installed: for default
// presets that means a quadlet on disk; for custom services that means a
// custom-service YAML. If neither is found ReinstallService returns an error.
//
// When resetData is true the data dir is renamed-aside (recoverable as
// .pre-remove-<ts>) and ReprovisionLinkedSites is invoked after the install
// completes so dependent sites' DBs/buckets exist on the fresh service.
func ReinstallService(name string, resetData bool, emit func(PhaseEvent)) error {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}
	emit(PhaseEvent{Phase: "reinstall_starting"})

	version, err := captureInstalledVersion(name)
	if err != nil {
		return err
	}

	if err := RemoveService(name, RemoveOptions{RemoveData: resetData}, emit); err != nil {
		return fmt.Errorf("reinstall: remove step: %w", err)
	}

	if _, err := reinstallInstallFn(name, version, emit); err != nil {
		return fmt.Errorf("reinstall: install step: %w", err)
	}

	if resetData {
		if err := reinstallReprovFn(name, emit); err != nil {
			return fmt.Errorf("reinstall: reprovision step: %w", err)
		}
	}
	return nil
}

// captureInstalledVersion returns the saved PresetVersion for multi-version
// presets (empty for single-version and default presets), so the reinstall
// resolves to the same image without false-rejecting an extracted tag.
func captureInstalledVersion(name string) (string, error) {
	if existing, err := config.LoadCustomService(name); err == nil {
		return existing.PresetVersion, nil
	}
	if config.IsDefaultPreset(name) && podman.QuadletInstalled("lerd-"+name) {
		return "", nil
	}
	return "", fmt.Errorf("service %q is not installed; nothing to reinstall", name)
}

// realReinstallInstall dispatches to the right install path based on whether
// name refers to a default preset or a custom service.
func realReinstallInstall(name, version string, emit func(PhaseEvent)) (*config.CustomService, error) {
	if config.IsDefaultPreset(name) {
		emit(PhaseEvent{Phase: "installing_config"})
		if err := EnsureDefaultPresetQuadlet(name); err != nil {
			return nil, err
		}
		_ = podman.DaemonReloadFn()

		unit := "lerd-" + name
		emit(PhaseEvent{Phase: "starting_unit", Unit: unit})
		var startErr error
		for attempt := range 5 {
			startErr = podman.StartUnit(unit)
			if startErr == nil || !strings.Contains(startErr.Error(), "not found") {
				break
			}
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
		}
		if startErr != nil {
			return nil, startErr
		}
		_ = config.SetServicePaused(name, false)
		_ = config.SetServiceManuallyStarted(name, true)

		emit(PhaseEvent{Phase: "waiting_ready", Unit: unit})
		if err := podman.WaitReady(name, 60*time.Second); err != nil {
			return nil, err
		}
		return &config.CustomService{Name: name}, nil
	}
	return InstallPresetStreaming(name, version, emit)
}
