// Package serviceops contains the shared business logic for installing,
// starting, stopping, and removing lerd services. The CLI commands and the
// MCP tools both call into here so they enforce identical preset gating,
// dependency cascades, and dynamic_env regeneration.
package serviceops

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// builtinServices is the list of built-in service names that ship with lerd.
// Kept here so callers don't have to import cli to know what's built in.
var builtinServices = []string{"mysql", "redis", "postgres", "meilisearch", "rustfs", "mailpit"}

// IsBuiltin reports whether name is a built-in lerd service.
func IsBuiltin(name string) bool {
	for _, s := range builtinServices {
		if s == name {
			return true
		}
	}
	return false
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

// EnsureCustomServiceQuadlet writes the quadlet for a custom service and
// reloads systemd. Materialises any declared file mounts and resolves
// dynamic_env directives so the rendered quadlet has the computed values.
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
	if err := podman.WriteQuadlet(quadletName, content); err != nil {
		return fmt.Errorf("writing quadlet for %s: %w", svc.Name, err)
	}
	return podman.DaemonReload()
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
