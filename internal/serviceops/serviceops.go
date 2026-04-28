// Package serviceops contains the shared business logic for installing,
// starting, stopping, and removing lerd services. The CLI commands and the
// MCP tools both call into here so they enforce identical preset gating,
// dependency cascades, and dynamic_env regeneration.
package serviceops

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/registry"
)

// IsBuiltin reports whether name is a built-in (default-preset) lerd service.
// Kept as a passthrough so callers don't have to import config.
func IsBuiltin(name string) bool { return config.IsDefaultPreset(name) }

// PhaseEvent is one step of the streaming preset-install flow.
type PhaseEvent struct {
	Phase   string `json:"phase"`
	Image   string `json:"image,omitempty"`
	Message string `json:"message,omitempty"`
	Dep     string `json:"dep,omitempty"`
	State   string `json:"state,omitempty"`
	Unit    string `json:"unit,omitempty"`
}

// InstallPresetStreaming runs the full install flow and emits a PhaseEvent
// at every step. The image is pulled before StartUnit so the hidden
// on-demand pull latency becomes visible progress in the UI.
func InstallPresetStreaming(name, version string, emit func(PhaseEvent)) (*config.CustomService, error) {
	emit(PhaseEvent{Phase: "installing_config"})
	svc, err := InstallPresetByName(name, version)
	if err != nil {
		return nil, err
	}

	for _, dep := range svc.DependsOn {
		emit(PhaseEvent{Phase: "starting_deps", Dep: dep, State: "starting"})
		if err := EnsureServiceRunning(dep); err != nil {
			return svc, fmt.Errorf("starting dependency %q: %w", dep, err)
		}
		emit(PhaseEvent{Phase: "starting_deps", Dep: dep, State: "ready"})
	}

	if svc.Image != "" && !podman.ImageExists(svc.Image) {
		emit(PhaseEvent{Phase: "pulling_image", Image: svc.Image})
		pullErr := podman.PullImageWithProgress(svc.Image, func(line string) {
			emit(PhaseEvent{Phase: "pulling_image", Message: line})
		})
		if pullErr != nil {
			return svc, pullErr
		}
	}

	unit := "lerd-" + svc.Name
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
		return svc, startErr
	}
	_ = config.SetServicePaused(svc.Name, false)
	_ = config.SetServiceManuallyStarted(svc.Name, true)

	emit(PhaseEvent{Phase: "waiting_ready", Unit: unit})
	if err := podman.WaitReady(svc.Name, 60*time.Second); err != nil {
		return svc, err
	}
	return svc, nil
}

// InstallPresetByName materialises a bundled preset as a custom service.
// version selects a tag for multi-version presets; empty falls back to the
// preset's DefaultVersion.
func InstallPresetByName(name, version string) (*config.CustomService, error) {
	preset, err := config.LoadPreset(name)
	if err != nil {
		return nil, err
	}
	if version != "" && len(preset.Versions) == 0 {
		return nil, fmt.Errorf("preset %q does not declare versions", name)
	}
	svc, err := preset.Resolve(version)
	if err != nil {
		return nil, err
	}
	if IsBuiltin(svc.Name) {
		return nil, fmt.Errorf("%q collides with the built-in service of the same name", svc.Name)
	}
	if _, err := config.LoadCustomService(svc.Name); err == nil {
		return nil, fmt.Errorf("custom service %q already exists; remove it first with: lerd service remove %s", svc.Name, svc.Name)
	}
	if missing := MissingPresetDependencies(svc); len(missing) > 0 {
		return nil, fmt.Errorf("preset %q requires service(s) %s to be installed first", svc.Name, strings.Join(missing, ", "))
	}
	if err := config.SaveCustomService(svc); err != nil {
		return nil, fmt.Errorf("saving service config: %w", err)
	}
	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		return nil, fmt.Errorf("writing quadlet: %w", err)
	}
	if svc.Family != "" {
		RegenerateFamilyConsumers(svc.Family)
	}
	return svc, nil
}

// MissingPresetDependencies returns the names of services that svc declares
// in depends_on but which are neither built-in nor already installed as
// custom services.
func MissingPresetDependencies(svc *config.CustomService) []string {
	var missing []string
	for _, dep := range svc.DependsOn {
		if IsBuiltin(dep) {
			continue
		}
		if _, err := config.LoadCustomService(dep); err == nil {
			continue
		}
		missing = append(missing, dep)
	}
	return missing
}

// EnsureDefaultPresetQuadlet writes the quadlet for a default-preset service
// (mysql, postgres, redis, ...) by resolving the canonical CustomService from
// its YAML preset, layering the user's image / extra-port overrides from
// global config, applying the platform-specific image override last (matching
// the legacy "platform override wins" semantics), and finally writing through
// the shared custom-service quadlet writer.
//
// This replaces the older embedded-template flow (cli.ensureServiceQuadlet)
// so default services and add-on presets share one code path.
func EnsureDefaultPresetQuadlet(name string) error {
	if !config.IsDefaultPreset(name) {
		return fmt.Errorf("not a default preset: %q", name)
	}
	p, err := config.LoadPreset(name)
	if err != nil {
		return err
	}
	svc, err := p.Resolve("")
	if err != nil {
		return err
	}
	hasUserPin := false
	if cfg, loadErr := config.LoadGlobal(); loadErr == nil {
		if svcCfg, ok := cfg.Services[name]; ok {
			if svcCfg.Image != "" {
				svc.Image = svcCfg.Image
				hasUserPin = true
			}
			if len(svcCfg.ExtraPorts) > 0 {
				svc.Ports = append(svc.Ports, svcCfg.ExtraPorts...)
			}
		}
	}
	// track_latest: when there's no user pin, query the registry for the actual
	// newest tag in the current major + variant line. The YAML preset.Image
	// stays as a fallback when the registry is unreachable.
	if !hasUserPin && p.TrackLatest {
		if latest, _ := registry.NewestStable(svc.Image, p.AllowMajorUpgrade); latest != nil {
			if at := strings.LastIndex(svc.Image, ":"); at > 0 {
				svc.Image = svc.Image[:at] + ":" + latest.Name
			}
		}
	}
	p.ApplyPlatformOverride(svc, runtime.GOOS)
	return EnsureCustomServiceQuadlet(svc)
}

// EnsureCustomServiceQuadlet writes the quadlet for a custom service and
// reloads systemd only when the file actually changed on disk. Materialises
// any declared file mounts and resolves dynamic_env directives so the
// rendered quadlet has the computed values.
func EnsureCustomServiceQuadlet(svc *config.CustomService) error {
	if svc.DataDir != "" {
		if err := os.MkdirAll(config.DataSubDir(svc.Name), 0755); err != nil {
			return fmt.Errorf("creating data directory for %s: %w", svc.Name, err)
		}
	}
	if err := config.MaterializeServiceFiles(svc); err != nil {
		return err
	}
	if err := config.ResolveDynamicEnv(svc); err != nil {
		return err
	}
	content := podman.GenerateCustomQuadlet(svc)
	quadletName := "lerd-" + svc.Name
	changed, err := podman.WriteQuadletDiff(quadletName, content)
	if err != nil {
		return fmt.Errorf("writing unit for %s: %w", svc.Name, err)
	}
	return podman.DaemonReloadIfNeeded(changed)
}

// EnsureServiceRunning starts the service if it is not already active and
// waits until it is ready. Recurses through depends_on for custom services.
func EnsureServiceRunning(name string) error {
	unit := "lerd-" + name
	status, _ := podman.UnitStatus(unit)
	if status == "active" {
		if err := podman.WaitReady(name, 30*time.Second); err != nil {
			return fmt.Errorf("%s is active but not yet ready: %w", name, err)
		}
		return nil
	}
	if !IsBuiltin(name) {
		svc, err := config.LoadCustomService(name)
		if err != nil {
			return fmt.Errorf("custom service %q not found: %w", name, err)
		}
		for _, dep := range svc.DependsOn {
			if err := EnsureServiceRunning(dep); err != nil {
				return fmt.Errorf("starting dependency %q for %q: %w", dep, name, err)
			}
		}
		if err := EnsureCustomServiceQuadlet(svc); err != nil {
			return err
		}
	}
	if err := podman.StartUnit(unit); err != nil {
		return err
	}
	return podman.WaitReady(name, 60*time.Second)
}

// StartDependencies ensures every entry in svc.DependsOn is up and ready
// before the parent is started.
func StartDependencies(svc *config.CustomService) error {
	if svc == nil {
		return nil
	}
	for _, dep := range svc.DependsOn {
		if err := EnsureServiceRunning(dep); err != nil {
			return fmt.Errorf("starting dependency %q for %q: %w", dep, svc.Name, err)
		}
	}
	return nil
}

// StopWithDependents stops every custom service that depends on name
// (depth-first), then stops name itself.
func StopWithDependents(name string) {
	for _, dep := range config.CustomServicesDependingOn(name) {
		StopWithDependents(dep)
	}
	unit := "lerd-" + name
	status, _ := podman.UnitStatus(unit)
	if status == "active" || status == "activating" {
		fmt.Printf("Stopping %s...\n", unit)
		_ = podman.StopUnit(unit)
	}
}

// ServiceFamily returns the family of a service by name. Honours the
// explicit Family field on a custom service first, falls back to
// config.InferFamily for built-ins and pattern-matched alternates.
func ServiceFamily(name string) string {
	if svc, err := config.LoadCustomService(name); err == nil && svc.Family != "" {
		return svc.Family
	}
	return config.InferFamily(name)
}

// RegenerateFamilyConsumersForService is a convenience that wraps
// RegenerateFamilyConsumers in a no-op when name has no recognised family.
func RegenerateFamilyConsumersForService(name string) {
	if fam := ServiceFamily(name); fam != "" {
		RegenerateFamilyConsumers(fam)
	}
}

// RegenerateFamilyConsumers re-renders the quadlet of any installed custom
// service whose dynamic_env references the named family. Active consumers
// are stopped, removed, and started so the new generated unit is the one
// systemd loads.
func RegenerateFamilyConsumers(family string) {
	customs, err := config.ListCustomServices()
	if err != nil {
		return
	}
	for _, c := range customs {
		if !consumesFamily(c, family) {
			continue
		}
		if err := EnsureCustomServiceQuadlet(c); err != nil {
			fmt.Printf("  [WARN] regenerating %s quadlet: %v\n", c.Name, err)
			continue
		}
		unit := "lerd-" + c.Name
		status, _ := podman.UnitStatus(unit)
		if status != "active" && status != "activating" {
			continue
		}
		fmt.Printf("  Restarting %s to pick up updated %s family members...\n", unit, family)
		if err := podman.StopUnit(unit); err != nil {
			fmt.Printf("  [WARN] stopping %s: %v\n", unit, err)
		}
		podman.RemoveContainer(unit)
		if err := podman.StartUnit(unit); err != nil {
			fmt.Printf("  [WARN] starting %s: %v\n", unit, err)
		}
	}
}

func consumesFamily(svc *config.CustomService, family string) bool {
	for _, directive := range svc.DynamicEnv {
		parts := strings.SplitN(directive, ":", 2)
		if len(parts) != 2 || parts[0] != "discover_family" {
			continue
		}
		for _, fam := range strings.Split(parts[1], ",") {
			if strings.TrimSpace(fam) == family {
				return true
			}
		}
	}
	return false
}
