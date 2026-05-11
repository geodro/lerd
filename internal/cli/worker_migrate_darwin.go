//go:build darwin

package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
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
	return migrateWorkersOnModeChangeStreaming(fromMode, toMode, nil)
}

// migrateWorkersOnModeChangeStreaming is the streaming variant. emit is
// invoked at every meaningful step (saving, per-worker stop/clean/start,
// final sweep) so the dashboard modal can reflect live progress. nil emit
// is allowed for the CLI path that doesn't need streaming.
func migrateWorkersOnModeChangeStreaming(fromMode, toMode string, emit func(WorkerModePhaseEvent)) error {
	if emit == nil {
		emit = func(WorkerModePhaseEvent) {}
	}
	if fromMode == toMode {
		emit(WorkerModePhaseEvent{Phase: "done"})
		return nil
	}
	workerMigrationActive.Add(1)
	defer workerMigrationActive.Add(-1)
	units := discoverActiveWorkerUnits()
	// Quiesce competing podman traffic before we touch anything: cancel
	// open `podman logs -f` SSE streams in lerd-ui and pause the cache
	// poller. Otherwise those long-lived connections race the migration's
	// `podman rm -f` and saturate gvproxy, wedging the API socket. nil on
	// CLI (no UI streams, no poller).
	if BeforeWorkerMigration != nil {
		BeforeWorkerMigration(units)
	}
	if AfterWorkerMigration != nil {
		defer AfterWorkerMigration()
	}
	steps := planWorkerMigration(fromMode, toMode, units)
	if len(steps) == 0 {
		// Discovery saw no plists, but the runtime state may still be
		// dirty: a previous migration could have removed plists without
		// stopping their containers (kickstart interrupting in-flight
		// work, etc.). When landing in exec mode, run the orphan-container
		// sweep unconditionally so leftover container-mode workers don't
		// keep running with `--restart=always` after the toggle.
		if toMode == config.WorkerExecModeExec {
			emit(WorkerModePhaseEvent{Phase: "sweeping_orphans"})
			sweepOrphanWorkerContainers()
		}
		emit(WorkerModePhaseEvent{Phase: "done", Message: "no active workers to migrate"})
		return nil
	}

	for _, step := range steps {
		emit(WorkerModePhaseEvent{Phase: "migrating_worker", Unit: step.Unit, Step: "stopping"})
		// Stop in old shape. podman.StopUnit handles both container
		// quadlets and plain service units — it boots out of launchd
		// and stops the container if any.
		if err := podman.StopUnit(step.Unit); err != nil {
			fmt.Printf("[WARN] stopping %s: %v\n", step.Unit, err)
		}

		emit(WorkerModePhaseEvent{Phase: "migrating_worker", Unit: step.Unit, Step: "cleaning"})
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
		emit(WorkerModePhaseEvent{Phase: "migrating_worker", Unit: step.Unit, Step: "starting"})
		if err := restartWorkerByUnitName(step.Unit); err != nil {
			fmt.Printf("[WARN] restart %s in %s mode: %v\n", step.Unit, step.To, err)
		}
	}

	// Defensive sweep when landing in exec mode: discovery only sees
	// workers with a live plist, so a container left behind by a previous
	// mode flip on an older build (or by a worker that exited before we
	// looked) survives the per-step rm. exec mode never owns per-worker
	// containers, so any lerd-{queue,schedule,horizon,reverb,custom}-*
	// container is by definition stale and safe to drop.
	if toMode == config.WorkerExecModeExec {
		emit(WorkerModePhaseEvent{Phase: "sweeping_orphans"})
		sweepOrphanWorkerContainers()
	}
	emit(WorkerModePhaseEvent{Phase: "done"})
	return nil
}

// sweepOrphanWorkerContainers removes any container whose name matches a
// known worker prefix. Only safe to call when the target mode is exec.
func sweepOrphanWorkerContainers() {
	prefixes := workerContainerPrefixes()
	if len(prefixes) == 0 {
		return
	}
	out, err := exec.Command(podman.PodmanBin(), "ps", "-a", "--format", "{{.Names}}").Output()
	if err != nil {
		return
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		for _, pfx := range prefixes {
			if strings.HasPrefix(name, pfx) {
				podman.RemoveContainer(name)
				break
			}
		}
	}
}

// workerContainerPrefixes turns the unit globs ("lerd-queue-*") into
// container-name prefixes ("lerd-queue-") for substring matching.
func workerContainerPrefixes() []string {
	globs := workerUnitGlobs()
	prefixes := make([]string, 0, len(globs))
	for _, g := range globs {
		prefixes = append(prefixes, strings.TrimSuffix(g, "*"))
	}
	return prefixes
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
		removeWorkerExecArtifacts(unit)
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
	kind, siteName, wtBase, ok := splitWorkerUnit(rest)
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
	// Preserve the worktree dimension on restart: a unit named
	// lerd-vite-mysite-feat-x must be brought back at the worktree path,
	// not the parent's, otherwise the migration silently rebinds the
	// per-worktree unit to the parent's WorkingDirectory.
	startPath := site.Path
	startPHP := site.PHPVersion
	if wtBase != "" {
		if wts, derr := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain()); derr == nil {
			for _, wt := range wts {
				if filepath.Base(wt.Path) == wtBase {
					startPath = wt.Path
					startPHP = config.WorktreePHPVersion(wt.Path, site.PHPVersion)
					break
				}
			}
		}
	}
	return WorkerStartForSite(siteName, startPath, startPHP, kind, worker, true)
}
