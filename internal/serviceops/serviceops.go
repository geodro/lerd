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
// in depends_on but which are not currently installed. A built-in (default
// preset) counts as installed only when its quadlet is on disk: the legacy
// "built-ins are always available" assumption broke once
// `lerd service remove <builtin>` shipped, and a dependent install that
// later hits EnsureServiceRunning on a missing built-in would fail with no
// rollback (which is exactly what reinstall pre-flight is meant to prevent).
func MissingPresetDependencies(svc *config.CustomService) []string {
	var missing []string
	for _, dep := range svc.DependsOn {
		if IsBuiltin(dep) {
			if podman.QuadletInstalled("lerd-" + dep) {
				continue
			}
			missing = append(missing, dep)
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
	return EnsureDefaultPresetQuadletPinned(name, "")
}

// EnsureDefaultPresetQuadletPinned is the reinstall-aware sibling of
// EnsureDefaultPresetQuadlet. When pinnedImage is non-empty, it is used as
// the source-of-truth for the Image= line, taking precedence over both the
// preset.Image fallback and the on-disk preserved image. Reinstall captures
// the on-disk image *before* RemoveService deletes the quadlet, then passes
// it here so the fresh install pins the same tag the user was running —
// otherwise the rolling preset.Image bump that the v1.19.0-beta.6 fix was
// designed to prevent fires on every reinstall.
//
// Callers outside the reinstall path should use EnsureDefaultPresetQuadlet
// (which passes pinnedImage="").
func EnsureDefaultPresetQuadletPinned(name, pinnedImage string) error {
	if !config.IsDefaultPreset(name) {
		return fmt.Errorf("not a default preset: %q", name)
	}
	p, err := config.LoadPreset(name)
	if err != nil {
		return err
	}
	canonicalPin := ""
	pinnedUserImage := ""
	var extraPorts []string
	if cfg, loadErr := config.LoadGlobal(); loadErr == nil {
		if svcCfg, ok := cfg.Services[name]; ok {
			canonicalPin = svcCfg.CanonicalVersion
			pinnedUserImage = svcCfg.Image
			extraPorts = svcCfg.ExtraPorts
		}
	}
	hasUserPin := pinnedUserImage != ""
	// Backfill for pre-existing installs that pre-date this feature: if no
	// pin is recorded but a container is running, derive the major from the
	// installed image tag and pin against the matching version.
	if canonicalPin == "" && len(p.Versions) > 0 {
		var probe string
		if hasUserPin {
			probe = pinnedUserImage
		} else {
			probe = podman.InstalledImage("lerd-" + name)
		}
		if probe != "" {
			canonicalPin = matchVersionByImageTag(probe, p.Versions)
		}
	}
	var svc *config.CustomService
	if canonicalPin != "" && len(p.Versions) > 0 {
		svc, err = p.ResolvePinned(canonicalPin)
	} else {
		svc, err = p.Resolve("")
	}
	if err != nil {
		return err
	}
	if hasUserPin {
		svc.Image = pinnedUserImage
	}
	if len(extraPorts) > 0 {
		svc.Ports = append(svc.Ports, extraPorts...)
	}
	// First-install / backfill pin: persist the canonical tag so future YAML
	// canonical flips don't silently major-jump this install.
	if canonicalPin == "" && len(p.Versions) > 0 {
		canonicalPin = p.CanonicalTag()
	}
	if canonicalPin != "" {
		if cfg, _ := config.LoadGlobal(); cfg != nil {
			entry := cfg.Services[name]
			if entry.CanonicalVersion != canonicalPin {
				entry.CanonicalVersion = canonicalPin
				cfg.Services[name] = entry
				_ = config.SaveGlobal(cfg)
			}
		}
	}
	preservedExisting := false
	if pinnedImage != "" {
		// Reinstall path: preserve the user's pre-remove tag verbatim. Skip
		// the strategy / track_latest blocks below so a reinstall really
		// reinstalls "the same thing", not "the same thing + an upgrade".
		svc.Image = pinnedImage
		preservedExisting = true
	} else if !hasUserPin {
		// Honor the on-disk image when the preset's update_strategy says we
		// shouldn't auto-jump to a newer line. Without this, the install rewrite
		// (`lerd update` → `install --from-update` → this function) silently bumps
		// users from their installed minor (e.g. meilisearch v1.7.x) to whatever
		// the new preset.Image declares (v1.42), bypassing the per-service
		// migration UX that `lerd service update` enforces. Rolling-strategy
		// services (mailpit, rustfs, gotenberg) intentionally fall through to the
		// preset image and the track_latest block below.
		strategy := registry.Strategy(p.UpdateStrategy)
		if strategy == registry.StrategyPatch || strategy == registry.StrategyMinor || strategy == registry.StrategyNone {
			if installed := podman.InstalledImage("lerd-" + name); installed != "" {
				svc.Image = installed
				preservedExisting = true
				if strategy != registry.StrategyNone {
					if newer, _ := registry.MaybeNewerTag(installed, strategy); newer != nil {
						if at := strings.LastIndex(svc.Image, ":"); at > 0 {
							svc.Image = svc.Image[:at] + ":" + newer.Name
						}
					}
				}
			}
		}
	}
	// track_latest: when there's no user pin and we did not preserve an
	// existing on-disk image, query the registry for the actual newest tag
	// in the current major + variant line. The YAML preset.Image stays as a
	// fallback when the registry is unreachable.
	if !hasUserPin && !preservedExisting && p.TrackLatest {
		if latest, _ := registry.NewestStable(svc.Image, p.AllowMajorUpgrade); latest != nil {
			if at := strings.LastIndex(svc.Image, ":"); at > 0 {
				svc.Image = svc.Image[:at] + ":" + latest.Name
			}
		}
	}
	p.ApplyPlatformOverride(svc, runtime.GOOS)
	return EnsureCustomServiceQuadlet(svc)
}

// matchVersionByImageTag picks the longest version tag that is a prefix of
// the installed image's tag. Lets backfill recognise postgis:16.5-3.5-alpine
// as version "16" and mysql:8.4.9 as version "8.4".
func matchVersionByImageTag(image string, versions []config.PresetVersion) string {
	at := strings.LastIndex(image, ":")
	if at < 0 {
		return ""
	}
	tag := image[at+1:]
	best := ""
	for _, v := range versions {
		if tag == v.Tag || strings.HasPrefix(tag, v.Tag+".") || strings.HasPrefix(tag, v.Tag+"-") {
			if len(v.Tag) > len(best) {
				best = v.Tag
			}
		}
	}
	return best
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
func ServiceFamily(name string) string { return config.FamilyOfName(name) }

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
