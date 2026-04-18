//go:build darwin

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
)

// migrateWorkersOnModeChange applies the worker-mode flip to every active
// worker: stop it in its old shape, clean up stale artifacts for that
// shape, and let the caller (applyWorkersMode) re-run writeWorkerUnitFile
// via the normal start path so workers come back in the new shape.
//
// Scope deliberately narrow: only workers are touched, not FPM, nginx,
// services, watchers. That way toggling the mode is O(workers on this
// machine) rather than a full `lerd stop && lerd start`.
//
// fromMode is the mode workers were actually launched in; toMode is what
// the user just requested. Safe to call with fromMode == toMode (no-op).
func migrateWorkersOnModeChange(fromMode, toMode string) error {
	if fromMode == toMode {
		return nil
	}
	units := discoverActiveWorkerUnits()
	steps := planWorkerMigration(fromMode, toMode, units)
	if len(steps) == 0 {
		return nil
	}

	for _, step := range steps {
		// Stop in old shape. podman.StopUnit handles both container
		// quadlets and plain service units — it boots out of launchd
		// and stops the container if any.
		if err := podman.StopUnit(step.Unit); err != nil {
			fmt.Printf("[WARN] stopping %s: %v\n", step.Unit, err)
		}

		// Remove the old on-disk artifacts so the new shape doesn't
		// coexist with a stale one. Failures here are warnings — the
		// new start still takes effect, stale files just linger.
		removeOldWorkerArtifacts(step.Unit, step.OldKind)

		// Also remove the container itself if it's still around (true
		// after container → exec: the quadlet stops create the container
		// on each start, and stopping it doesn't remove it).
		podman.RemoveContainer(step.Unit)
	}

	// After cleanup, restart each worker by recomputing its definition
	// from the site's framework metadata. writeWorkerUnitFile sees the
	// new config and produces the correct shape.
	for _, step := range steps {
		if err := restartWorkerByUnitName(step.Unit); err != nil {
			fmt.Printf("[WARN] restart %s in %s mode: %v\n", step.Unit, step.To, err)
		}
	}
	return nil
}

// discoverActiveWorkerUnits returns the unit names of every lerd framework
// worker the service manager currently knows about, in either shape. We
// union services + container units so the migration catches workers
// regardless of which mode they were started under.
func discoverActiveWorkerUnits() []string {
	seen := map[string]bool{}
	all := []string{}
	for _, glob := range workerUnitGlobs() {
		for _, u := range services.Mgr.ListServiceUnits(glob) {
			if !seen[u] {
				seen[u] = true
				all = append(all, u)
			}
		}
		for _, u := range services.Mgr.ListContainerUnits(glob) {
			if !seen[u] {
				seen[u] = true
				all = append(all, u)
			}
		}
	}
	return all
}

// workerUnitGlobs enumerates the name patterns the built-in and custom
// framework workers use. Kept in one place so new worker kinds show up
// here first (queue / schedule / reverb / horizon are built-in; everything
// else is framework-declared and prefixed by the framework's own name).
func workerUnitGlobs() []string {
	builtins := []string{
		"lerd-queue-*",
		"lerd-schedule-*",
		"lerd-horizon-*",
		"lerd-reverb-*",
	}
	// Custom framework workers live under lerd-<worker-name>-<site>.
	// We can't glob every framework; fall back to enumerating the
	// registered ones from config.
	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return builtins
	}
	seen := map[string]bool{}
	for _, s := range reg.Sites {
		if s.Ignored {
			continue
		}
		fw, ok := config.GetFramework(s.Framework)
		if !ok || fw == nil {
			continue
		}
		for name := range fw.Workers {
			switch name {
			case "queue", "schedule", "horizon", "reverb":
				continue
			}
			if !seen[name] {
				seen[name] = true
				builtins = append(builtins, "lerd-"+name+"-*")
			}
		}
	}
	return builtins
}

// removeOldWorkerArtifacts deletes the on-disk files the worker held in
// `kind` shape. In container mode that's the quadlet + any leftover
// container; in exec mode that's the service unit + launchd plist +
// guard script + pid file.
func removeOldWorkerArtifacts(unit string, kind workerArtifactKind) {
	switch kind {
	case artifactContainer:
		_ = services.Mgr.RemoveContainerUnit(unit)
	case artifactService:
		_ = services.Mgr.RemoveServiceUnit(unit)
		workersDir := filepath.Join(config.RunDir(), "workers")
		// Guard script and pid file — non-fatal if either is missing.
		_ = os.Remove(filepath.Join(workersDir, unit+".sh"))
		_ = os.Remove(filepath.Join(workersDir, unit+".pid"))
	}
}

// restartWorkerByUnitName parses a worker unit name (lerd-<kind>-<site>)
// back into its kind + site, looks up the site's framework worker, and
// calls the normal WorkerStartForSite code path. writeWorkerUnitFile
// inside picks the current configured mode.
func restartWorkerByUnitName(unit string) error {
	const prefix = "lerd-"
	if !strings.HasPrefix(unit, prefix) {
		return fmt.Errorf("unit %q does not start with %s", unit, prefix)
	}
	rest := unit[len(prefix):]
	// Split on the last hyphen: worker names can themselves contain
	// hyphens ("custom-worker-mysite") while site names should not
	// contain hyphens within the short registry form, but we match
	// "<kind>-<siteName>" by matching the site name first.
	kind, siteName, ok := splitWorkerUnit(rest)
	if !ok {
		return fmt.Errorf("could not parse worker unit %q", unit)
	}
	site, err := config.FindSite(siteName)
	if err != nil {
		return fmt.Errorf("site %q not found: %w", siteName, err)
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok || fw == nil {
		return fmt.Errorf("no framework for site %q", siteName)
	}
	worker, ok := fw.Workers[kind]
	if !ok {
		return fmt.Errorf("worker %q not defined for framework %q", kind, fw.Label)
	}
	return WorkerStartForSite(siteName, site.Path, site.PHPVersion, kind, worker)
}

// splitWorkerUnit splits "<kind>-<siteName>" into (kind, siteName, ok).
// Uses config.LoadSites to find a matching site, so kinds with hyphens
// (e.g. "laravel-reverb") work as long as the site name itself is a
// registered site.
func splitWorkerUnit(rest string) (kind, siteName string, ok bool) {
	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return "", "", false
	}
	for _, s := range reg.Sites {
		suffix := "-" + s.Name
		if strings.HasSuffix(rest, suffix) {
			kind = strings.TrimSuffix(rest, suffix)
			return kind, s.Name, kind != ""
		}
	}
	return "", "", false
}
