package serviceops

import (
	"errors"
	"fmt"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// Seams so tests can drive reconcile without real podman/quadlet work.
var (
	ensureQuadletFn          = EnsureCustomServiceQuadlet
	listManagedServiceNames  = podman.ListManagedServiceNames
	orphanContainerRunningFn = podman.ContainerRunningQuiet
)

// ReconcileResult reports what ReconcileServices changed.
type ReconcileResult struct {
	QuadletsRegenerated   []string // YAML present, unit was missing and regenerated
	OrphansRemoved        []string // service quadlet with no YAML, removed
	RunningOrphansSkipped []string // orphan left in place because its container is up
}

// ReconcileServices enforces the issue #678 invariant: regenerate a missing
// unit from its YAML, remove an orphan service quadlet (no YAML; data left).
// A running orphan is skipped, not destroyed. Per-item errors are collected so
// one bad service can't block the rest.
func ReconcileServices(emit func(PhaseEvent)) (ReconcileResult, error) {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}
	var res ReconcileResult
	var errs []error

	// Forward: every defined service must have its unit (EnsureCustomServiceQuadlet
	// also re-syncs the macOS plist).
	customs, err := config.ListCustomServices()
	if err != nil {
		return res, fmt.Errorf("listing custom services: %w", err)
	}
	for _, svc := range customs {
		if UnitInstalledFn("lerd-" + svc.Name) {
			continue
		}
		if err := ensureQuadletFn(svc); err != nil {
			errs = append(errs, fmt.Errorf("regenerating quadlet for %s: %w", svc.Name, err))
			continue
		}
		res.QuadletsRegenerated = append(res.QuadletsRegenerated, svc.Name)
	}

	// Backward: a service quadlet with no YAML is an orphan. Candidates are marked
	// quadlets (any name, covers user-defined services) plus known non-default
	// preset names (covers legacy orphans written before the marker existed).
	for _, name := range orphanCandidates() {
		if config.IsDefaultPreset(name) || config.CustomServiceExists(name) {
			continue
		}
		if !podman.QuadletInstalled("lerd-" + name) {
			continue
		}
		// Don't destroy a workload that's still up; leave it for explicit removal.
		if orphanContainerRunningFn("lerd-" + name) {
			res.RunningOrphansSkipped = append(res.RunningOrphansSkipped, name)
			continue
		}
		if err := RemoveService(name, RemoveOptions{}, emit); err != nil {
			errs = append(errs, fmt.Errorf("removing orphan service %s: %w", name, err))
			continue
		}
		res.OrphansRemoved = append(res.OrphansRemoved, name)
	}
	return res, errors.Join(errs...)
}

// orphanCandidates is the deduped set of service names worth checking for an
// orphan quadlet: marked quadlets present on disk, plus every non-default preset
// name (so legacy pre-marker orphans are still reaped). Preset names never
// collide with site/worker unit names, so this stays safe.
func orphanCandidates() []string {
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, n := range listManagedServiceNames() {
		add(n)
	}
	presets, err := config.ListPresets()
	if err == nil {
		for _, p := range presets {
			if config.IsDefaultPreset(p.Name) {
				continue
			}
			add(p.Name)
			for _, v := range p.Versions {
				add(config.PresetVersionServiceName(p.Name, v))
			}
		}
	}
	return names
}
