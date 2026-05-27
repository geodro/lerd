package serviceops

import (
	"fmt"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// reinstallSpec captures every piece of state we need to reproduce after
// RemoveService runs. Built before RemoveService so the fresh install can
// pin the same image / version the user was running, instead of re-resolving
// the rolling preset.Image and silently jumping versions.
type reinstallSpec struct {
	// presetName is the bundled preset this service was installed from.
	// Differs from the service name for non-canonical versions, e.g.
	// service "mariadb-10-11" comes from preset "mariadb". For default
	// presets and canonical-version services this equals name.
	presetName string
	// version is the multi-version preset tag (e.g. "8.4" for mysql).
	// Empty for single-version presets and for default presets installed
	// before the canonical-pin field existed.
	version string
	// image is the fully-qualified Image= line from the on-disk quadlet
	// (e.g. "docker.io/library/mysql:8.4.9"). Empty when there's no quadlet.
	image string
}

// Seams for ReinstallService composition. The defaults wire to the real
// install + reprovision + validation functions; tests substitute fakes so
// composition logic can be exercised without a real preset bundle or podman.
var (
	reinstallInstallFn       = realReinstallInstall
	reinstallReprovFn        = ReprovisionLinkedSites
	reinstallValidateFn      = validateReinstall
	reinstallPrefetchImageFn = realPrefetchImage
	// reinstallStreamingFn lets tests of realReinstallInstall verify the
	// custom-service path passes spec.presetName (NOT the service name) to
	// the install streaming function, which is the actual routing fix.
	reinstallStreamingFn = InstallPresetStreaming
	// reinstallFamilyRegenFn drives family-consumer regeneration outside
	// of InstallPresetByName's internal call: default-preset reinstalls
	// (where the install path doesn't regen) and the install-failure path
	// (where consumers must sync to the post-Remove "target gone" state).
	reinstallFamilyRegenFn = RegenerateFamilyConsumersForService
)

// realPrefetchImage pulls the pinned image when it isn't already local so a
// network/registry failure surfaces before RemoveService deletes anything.
// Always emits `preflight_image_ok` on success so callers driving a UI off
// the event stream see a consistent terminal phase on both the warm-cache
// and the cold-cache path.
func realPrefetchImage(image string, emit func(PhaseEvent)) error {
	if image == "" {
		return nil
	}
	if podman.ImageExists(image) {
		emit(PhaseEvent{Phase: "preflight_image_ok", Image: image})
		return nil
	}
	emit(PhaseEvent{Phase: "pulling_image", Image: image})
	if err := podman.PullImageWithProgress(image, func(line string) {
		emit(PhaseEvent{Phase: "pulling_image", Message: line})
	}); err != nil {
		return err
	}
	emit(PhaseEvent{Phase: "preflight_image_ok", Image: image})
	return nil
}

// ReinstallService stops, removes, and reinstalls a service, optionally wiping
// its data and reprovisioning per-site state (databases, buckets) on the
// fresh service.
//
// Reinstall requires that the service is currently installed: for default
// presets that means a quadlet on disk; for custom services that means a
// custom-service YAML. If neither is found ReinstallService returns an error.
//
// Pre-flight validation runs before RemoveService so configuration errors
// (unknown preset, bad version, missing dependencies, unreachable image)
// fail with the on-disk state intact rather than after the service config
// has already been deleted.
//
// When resetData is true the data dir is renamed-aside (recoverable as
// .pre-remove-<ts>) and ReprovisionLinkedSites is invoked after the install
// completes so dependent sites' DBs/buckets exist on the fresh service.
func ReinstallService(name string, resetData bool, emit func(PhaseEvent)) error {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}
	emit(PhaseEvent{Phase: "reinstall_starting"})

	spec, err := captureReinstallSpec(name)
	if err != nil {
		return err
	}

	if err := reinstallValidateFn(name, spec); err != nil {
		return fmt.Errorf("reinstall: pre-flight: %w", err)
	}

	if err := reinstallPrefetchImageFn(spec.image, emit); err != nil {
		return fmt.Errorf("reinstall: pre-flight pull %q: %w", spec.image, err)
	}

	// Suppress regen-during-remove; we drive it ourselves below to
	// eliminate the launchctl bootout/bootstrap race on macOS.
	if err := RemoveService(name, RemoveOptions{RemoveData: resetData, SkipFamilyRegen: true}, emit); err != nil {
		return fmt.Errorf("reinstall: remove step: %w", err)
	}

	if _, err := reinstallInstallFn(name, spec, emit); err != nil {
		// Sync consumers to the post-Remove state so their plists stop
		// referencing the deleted target.
		reinstallFamilyRegenFn(name)
		return fmt.Errorf("reinstall: install step: %w", err)
	}

	// InstallPresetByName regenerates internally for custom services;
	// default-preset install doesn't, so do it here.
	if config.IsDefaultPreset(name) {
		reinstallFamilyRegenFn(name)
	}

	if resetData {
		if err := reinstallReprovFn(name, emit); err != nil {
			return fmt.Errorf("reinstall: reprovision step: %w", err)
		}
	}
	return nil
}

// captureReinstallSpec snapshots the on-disk state RemoveService is about to
// destroy, so realReinstallInstall can pin the same preset + version + image.
// For custom services the values come from the YAML; for default presets we
// only have the rendered quadlet's Image line and the service name itself.
func captureReinstallSpec(name string) (reinstallSpec, error) {
	if existing, err := config.LoadCustomService(name); err == nil {
		// Backfill the preset reference for legacy YAMLs written before the
		// Preset field existed: fall back to the service name, which is the
		// canonical preset name for single-version presets.
		presetName := existing.Preset
		if presetName == "" {
			presetName = name
		}
		// Multi-version presets must always carry a PresetVersion. An empty
		// value here is a YAML corruption: falling through to InstallPresetByName
		// with version="" would silently resolve to DefaultVersion and move
		// the user off whatever tag they were running.
		if preset, perr := config.LoadPreset(presetName); perr == nil && len(preset.Versions) > 0 && existing.PresetVersion == "" {
			return reinstallSpec{}, fmt.Errorf("service %q has empty preset_version; refusing to reinstall and silently jump to default version", name)
		}
		return reinstallSpec{
			presetName: presetName,
			version:    existing.PresetVersion,
			image:      existing.Image,
		}, nil
	}
	if config.IsDefaultPreset(name) && podman.QuadletInstalled("lerd-"+name) {
		return reinstallSpec{
			presetName: name,
			image:      podman.InstalledImage("lerd-" + name),
		}, nil
	}
	return reinstallSpec{}, fmt.Errorf("service %q is not installed; nothing to reinstall", name)
}

// validateReinstall replays the preset/version/dependency checks the install
// path will run, without writing anything. Catches configuration errors that
// would otherwise fail after RemoveService has already deleted the YAML and
// quadlet, leaving the user with neither the old install nor the new one.
//
// Default-preset and custom-service paths share the same outer validation,
// but the version-selection step mirrors each install path so the resolved
// svc.DependsOn matches what the install will actually run against:
//
//   - custom service: install calls InstallPresetStreaming(spec.presetName,
//     spec.version, ...) → preset.Resolve(version);
//   - default preset: install calls EnsureDefaultPresetQuadletPinned which
//     uses ResolvePinned(canonicalPin) from cfg.Services[name].CanonicalVersion,
//     falling back to Resolve("") when no pin is recorded.
func validateReinstall(name string, spec reinstallSpec) error {
	if spec.presetName == "" {
		return fmt.Errorf("service %q has no preset reference; cannot determine reinstall source", name)
	}
	preset, err := config.LoadPreset(spec.presetName)
	if err != nil {
		hint := ""
		if spec.presetName == name {
			hint = " (legacy YAML without `preset:` field; add `preset: <name>` pointing at a bundled preset)"
		}
		return fmt.Errorf("preset %q (source of service %q) not found%s: %w", spec.presetName, name, hint, err)
	}
	if spec.version != "" && len(preset.Versions) == 0 {
		return fmt.Errorf("preset %q does not declare versions, but service %q has preset_version=%q", spec.presetName, name, spec.version)
	}
	svc, err := resolveForValidate(name, spec, preset)
	if err != nil {
		return err
	}
	if missing := MissingPresetDependencies(svc); len(missing) > 0 {
		return fmt.Errorf("preset %q requires %s to be installed first", spec.presetName, strings.Join(missing, ", "))
	}
	return nil
}

// resolveForValidate picks the same preset version that the install path
// would pick, so MissingPresetDependencies runs against the right svc.
// Default presets read the canonical pin from global config (mirrors
// EnsureDefaultPresetQuadletPinned); other paths use the captured version.
func resolveForValidate(name string, spec reinstallSpec, preset *config.Preset) (*config.CustomService, error) {
	if config.IsDefaultPreset(name) && len(preset.Versions) > 0 {
		canonicalPin := ""
		if cfg, _ := config.LoadGlobal(); cfg != nil {
			canonicalPin = cfg.Services[name].CanonicalVersion
		}
		if canonicalPin != "" {
			svc, err := preset.ResolvePinned(canonicalPin)
			if err != nil {
				return nil, fmt.Errorf("preset %q canonical pin %q: %w", spec.presetName, canonicalPin, err)
			}
			return svc, nil
		}
	}
	svc, err := preset.Resolve(spec.version)
	if err != nil {
		return nil, fmt.Errorf("preset %q version %q: %w", spec.presetName, spec.version, err)
	}
	return svc, nil
}

// realReinstallInstall dispatches to the right install path based on whether
// name refers to a default preset or a custom service. For default presets we
// inject the captured image so EnsureDefaultPresetQuadlet pins the same tag
// the user was running, sidestepping the rolling preset.Image bump that
// EnsureDefaultPresetQuadlet would otherwise apply now that RemoveService has
// deleted the on-disk quadlet (InstalledImage returns "" mid-reinstall).
func realReinstallInstall(name string, spec reinstallSpec, emit func(PhaseEvent)) (*config.CustomService, error) {
	if config.IsDefaultPreset(name) {
		emit(PhaseEvent{Phase: "installing_config"})
		if err := EnsureDefaultPresetQuadletPinned(name, spec.image); err != nil {
			return nil, err
		}
		_ = podman.DaemonReloadFn()

		// Pull the pinned image up-front so a missing/unreachable registry
		// surfaces here as a clear error instead of a 60s WaitReady timeout.
		if spec.image != "" && !podman.ImageExists(spec.image) {
			emit(PhaseEvent{Phase: "pulling_image", Image: spec.image})
			if err := podman.PullImageWithProgress(spec.image, func(line string) {
				emit(PhaseEvent{Phase: "pulling_image", Message: line})
			}); err != nil {
				return nil, fmt.Errorf("pulling %s: %w", spec.image, err)
			}
		}

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
	return reinstallStreamingFn(spec.presetName, spec.version, emit)
}
