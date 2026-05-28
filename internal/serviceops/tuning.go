package serviceops

import (
	"errors"
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// Sentinel errors returned by SaveTuningOverride so callers (CLI, HTTP
// handler, future MCP surface) map them to a consistent error surface
// without each one re-deriving the install/family checks. Wrapped via
// %w so callers stay free to add context (e.g. the name) before
// returning.
var (
	// ErrTuningServiceNotInstalled means the service has no quadlet on
	// disk. Surfaces as 404 in HTTP. Lets default-preset names that
	// resolve through LoadPreset still error cleanly when the user has
	// explicitly `lerd service remove`d them, so an edit cannot silently
	// reinstall via the regen+restart path below.
	ErrTuningServiceNotInstalled = errors.New("service is not installed")
	// ErrTuningFamilyUnsupported means the service has no tuningMounts
	// entry for its family. Surfaces as 400 in HTTP.
	ErrTuningFamilyUnsupported = errors.New("service does not support tuning")
)

// SaveTuningOverride is the single entry point for writing the user
// tuning override file (`config.ServiceTuningFile(name)`), regenerating
// the quadlet so the override Volume= mount is present on installs
// predating the feature, and restarting the unit so it re-reads the
// config. Shared by the `lerd service config` CLI command and the
// `/api/services/{name}/config` HTTP handler; matches the pattern of
// xdebugops.Apply.
//
// Order:
//  1. ServiceInstalled guard — block silent-reinstall-on-edit for
//     removed default presets that ResolveServiceForTuning would
//     otherwise still resolve via LoadPreset.
//  2. ResolveServiceForTuning + ServiceTuningMount — fail fast with
//     family-unsupported for services that don't expose a mount.
//  3. MaterializeServiceTuning — seed the template on first save.
//  4. Write `content` to the host override file.
//  5. EnsureDefaultPresetQuadlet (built-ins) or EnsureCustomServiceQuadlet
//     (custom YAMLs). Failures are returned, NOT logged-and-ignored —
//     a freshly written file silently orphaned by a regen WARN is the
//     exact "green save, nothing changed" bug surfaced on #429 / #436.
//  6. RestartUnit so the container re-reads the override.
func SaveTuningOverride(name, content string) error {
	if !ServiceInstalled(name) {
		return fmt.Errorf("%w: run `lerd service preset install %s` first", ErrTuningServiceNotInstalled, name)
	}
	svc, err := config.ResolveServiceForTuning(name)
	if err != nil {
		// ServiceInstalled passed (quadlet on disk) but ResolveServiceForTuning
		// failed — orphan quadlet, treat as not-installed for a consistent
		// 404 instead of leaking a 500 from the lookup.
		return fmt.Errorf("%w: %s", ErrTuningServiceNotInstalled, err.Error())
	}
	if _, ok := config.ServiceTuningMount(svc); !ok {
		return fmt.Errorf("%w (family %q)", ErrTuningFamilyUnsupported, config.FamilyOf(svc))
	}
	if err := config.MaterializeServiceTuning(svc); err != nil {
		return fmt.Errorf("creating tuning file: %w", err)
	}
	path := config.ServiceTuningFile(name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing tuning file: %w", err)
	}
	if err := EnsureTuningQuadlet(name, svc); err != nil {
		return err
	}
	if err := podman.RestartUnit("lerd-" + name); err != nil {
		return fmt.Errorf("restarting %s: %w", "lerd-"+name, err)
	}
	return nil
}

// EnsureTuningQuadlet rewrites the quadlet for `name` so the tuning
// override Volume= mount is present on installs predating the feature.
// Built-in default presets regenerate through EnsureDefaultPresetQuadlet
// (which itself resolves to EnsureCustomServiceQuadlet, so the mount
// lands either way); custom-YAML services regenerate through
// EnsureCustomServiceQuadlet directly.
//
// Split out from SaveTuningOverride so the CLI's `lerd service config`
// (whose editor writes to the override file out-of-band, and which has
// a `--no-restart` flag) can share the regen step without forcing a
// restart. Failures are propagated, NOT logged-and-ignored — skipping
// the regen orphans the user's just-written override and the next
// restart would re-read the OLD config (no mount, no values picked up).
func EnsureTuningQuadlet(name string, svc *config.CustomService) error {
	if config.IsDefaultPreset(name) {
		if err := EnsureDefaultPresetQuadlet(name); err != nil {
			return fmt.Errorf("regenerating quadlet for %s: %w", name, err)
		}
		return nil
	}
	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		return fmt.Errorf("regenerating quadlet for %s: %w", name, err)
	}
	return nil
}
