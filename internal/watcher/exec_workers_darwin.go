//go:build darwin

package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
)

// WatchExecWorkers self-heals macOS framework workers when the configured
// runtime is `exec`. Container mode self-heals via podman's `--restart=always`,
// but exec-mode workers run as foreground guard scripts under launchd: a
// plist that gets booted out (e.g. mid-flight migration interrupted by a
// kickstart) leaves nothing for launchd to resurrect.
//
// Three classes of drift get repaired on each tick:
//
//  1. site declares a worker but no plist exists in LaunchAgents → write
//     the plist + bootstrap by calling cli.WorkerStartForSite.
//  2. plist exists but launchctl shows no PID and the .pid file is dead
//     → re-run WorkerStartForSite (RemoveServiceUnit + bootstrap).
//  3. orphan .sh/.pid in run/workers with no matching plist → delete.
//
// Skipped while a worker-mode migration is in flight (cli.WorkerMigrationActive)
// so a tick that lands between the migration's stop loop and start loop
// doesn't clobber the half-applied state with a stale shape.
//
// Per-unit cooldown prevents heal storms when a worker crashes on launch
// (e.g. FPM container down): we wait at least 2 minutes before retrying
// the same unit so launchd's own throttling has room to work.
func WatchExecWorkers(interval time.Duration) {
	const minHealInterval = 2 * time.Minute
	cooldown := map[string]time.Time{}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if cli.WorkerMigrationActive() {
			continue
		}
		cfg, err := config.LoadGlobal()
		if err != nil {
			continue
		}
		if cfg.WorkerExecMode() != config.WorkerExecModeExec {
			continue
		}

		expected := expectedExecWorkers()
		expectedNames := make(map[string]bool, len(expected))
		for _, w := range expected {
			expectedNames[w.unit] = true
		}
		sweepOrphanWorkerArtifacts(expectedNames)

		for _, w := range expected {
			if cli.WorkerMigrationActive() {
				break
			}
			if last, ok := cooldown[w.unit]; ok && time.Since(last) < minHealInterval {
				continue
			}
			// Skip schedule-style workers that declare a Schedule field —
			// those route through systemd .timer units, not the launchd
			// guard-script path this watcher handles.
			if w.def.Schedule != "" {
				continue
			}
			// Skip workers whose ConflictsWith counterpart is already
			// running for this site. Otherwise healing horizon would stop
			// queue, the next iteration would heal queue and stop horizon,
			// leaving one of the pair with an orphaned .sh file.
			if conflictingWorkerRunning(w) {
				continue
			}
			reason := workerNeedsHealing(w.unit)
			if reason == "" {
				continue
			}
			cooldown[w.unit] = time.Now()
			logger.Warn("self-healing exec-mode worker",
				"unit", w.unit, "reason", reason)
			if err := cli.WorkerStartForSite(w.site, w.sitePath, w.phpVersion, w.kind, w.def, true); err != nil {
				logger.Error("self-heal failed",
					"unit", w.unit, "err", err)
			}
		}
	}
}

type expectedExecWorker struct {
	unit       string
	site       string
	sitePath   string
	phpVersion string
	kind       string
	def        config.FrameworkWorker
}

// expectedExecWorkers walks the site registry and returns one entry per
// worker the site has explicitly enabled in .lerd.yaml's `workers:` list,
// honoring site-wide and per-worker pause flags. Mirrors the enumeration
// `lerd start` does so the watcher's idea of "what should be running"
// matches what the user has actually opted into — not the union of every
// framework-declared worker, which would heal workers the user disabled
// (e.g. reverb on a site without laravel/reverb installed).
func expectedExecWorkers() []expectedExecWorker {
	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return nil
	}
	cfg, _ := config.LoadGlobal()
	defaultPHP := ""
	if cfg != nil {
		defaultPHP = cfg.PHP.DefaultVersion
	}

	out := make([]expectedExecWorker, 0)
	for _, s := range reg.Sites {
		if s.Ignored || s.Paused {
			continue
		}
		fw, ok := config.GetFrameworkForDir(s.Framework, s.Path)
		if !ok || fw == nil || len(fw.Workers) == 0 {
			continue
		}
		proj, _ := config.LoadProjectConfig(s.Path)
		enabled := siteEnabledWorkers(fw, proj)
		if len(enabled) == 0 {
			continue
		}
		paused := make(map[string]bool, len(s.PausedWorkers))
		for _, name := range s.PausedWorkers {
			paused[name] = true
		}
		php := s.PHPVersion
		if php == "" {
			php = defaultPHP
		}
		for _, kind := range enabled {
			if paused[kind] {
				continue
			}
			def, ok := fw.Workers[kind]
			if !ok {
				continue
			}
			out = append(out, expectedExecWorker{
				unit:       "lerd-" + kind + "-" + s.Name,
				site:       s.Name,
				sitePath:   s.Path,
				phpVersion: php,
				kind:       kind,
				def:        def,
			})
		}

		// Per-worktree host workers (vite, etc.) get a unit per worktree
		// (lerd-<kind>-<site>-<wtBase>) under PR #319. The self-heal loop
		// must enumerate them too — without this, a worktree unit booted
		// out of launchd never recovers because the watcher pretends it
		// doesn't exist.
		wts, derr := gitpkg.DetectWorktrees(s.Path, s.PrimaryDomain())
		if derr != nil || len(wts) == 0 {
			continue
		}
		for _, wt := range wts {
			wtPHP := config.WorktreePHPVersion(wt.Path, php)
			for _, kind := range cli.OptedInHostWorkers(&s, wt.Path) {
				if paused[kind] {
					continue
				}
				def, ok := fw.Workers[kind]
				if !ok {
					continue
				}
				out = append(out, expectedExecWorker{
					unit:       "lerd-" + kind + "-" + s.Name + "-" + filepath.Base(wt.Path),
					site:       s.Name,
					sitePath:   wt.Path,
					phpVersion: wtPHP,
					kind:       kind,
					def:        def,
				})
			}
		}
	}
	return out
}

// siteEnabledWorkers picks the worker names the site has actually opted
// into. Empty .lerd.yaml `workers:` falls back to every framework worker —
// matches the behavior of `lerd worker start <name>` resolving against
// fw.Workers when the site hasn't been explicitly configured.
func siteEnabledWorkers(fw *config.Framework, proj *config.ProjectConfig) []string {
	if proj != nil && len(proj.Workers) > 0 {
		return proj.Workers
	}
	names := make([]string, 0, len(fw.Workers))
	for name := range fw.Workers {
		names = append(names, name)
	}
	return names
}

// conflictingWorkerRunning checks whether any of w's declared conflicts
// (e.g. horizon's ConflictsWith=[queue]) is currently bootstrapped under
// launchd for the same site. If so, this worker is intentionally stopped
// and shouldn't be healed — its rival owns the slot.
func conflictingWorkerRunning(w expectedExecWorker) bool {
	for _, conflict := range w.def.ConflictsWith {
		conflictUnit := "lerd-" + conflict + "-" + w.site
		if launchctlPID(conflictUnit) != "" {
			return true
		}
	}
	return false
}

// workerNeedsHealing returns a non-empty reason string when unit is in a
// drifted state that warrants a re-call of WorkerStartForSite. Empty string
// means the worker looks healthy.
func workerNeedsHealing(unit string) string {
	home, _ := os.UserHomeDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "lerd."+unit+".plist")
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return "plist missing"
	}
	pid := launchctlPID(unit)
	if pid == "" {
		return "not loaded in launchd"
	}
	if pid == "-" {
		// Loaded but not currently running. The exec-mode plist runs the
		// guard script in the foreground, so a "-" PID with no live .pid
		// indicates the script exited and launchd hasn't relaunched yet.
		// Healthy windows for "-" are short; if it persists past the
		// cooldown the next tick will trigger a heal regardless.
		if !pidFileLive(unit) {
			return "loaded but no live process"
		}
	}
	return ""
}

// launchctlPID returns the PID column from `launchctl list <label>` output,
// or empty string if the unit isn't bootstrapped in the GUI domain.
func launchctlPID(unit string) string {
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return ""
	}
	label := "com.lerd." + unit
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == label {
			return fields[0]
		}
	}
	return ""
}

// pidFileLive reads run/workers/<unit>.pid and returns true if the PID it
// contains is currently alive. A stale .pid (process gone, file lingering
// because the EXIT trap couldn't fire after `exec`) returns false so
// workerNeedsHealing flags the unit for relaunch.
func pidFileLive(unit string) bool {
	pidPath := filepath.Join(config.RunDir(), "workers", unit+".pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}
	pid := strings.TrimSpace(string(data))
	if pid == "" {
		return false
	}
	return exec.Command("kill", "-0", pid).Run() == nil
}

// sweepOrphanWorkerArtifacts deletes .sh + .pid files in run/workers whose
// unit isn't in the expected set. Keeps the directory in sync with the
// active site/framework topology so an old worker name (site renamed,
// worker removed from framework, site unlinked) doesn't leave forever-
// orphaned guard scripts behind.
func sweepOrphanWorkerArtifacts(expected map[string]bool) {
	dir := filepath.Join(config.RunDir(), "workers")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		var unit string
		switch {
		case strings.HasSuffix(name, ".sh"):
			unit = strings.TrimSuffix(name, ".sh")
		case strings.HasSuffix(name, ".pid"):
			unit = strings.TrimSuffix(name, ".pid")
		default:
			continue
		}
		if expected[unit] {
			continue
		}
		// Also keep artifacts whose plist still exists — sweeping those
		// would race a freshly-bootstrapped worker mid-launch.
		home, _ := os.UserHomeDir()
		if _, err := os.Stat(filepath.Join(home, "Library", "LaunchAgents", "lerd."+unit+".plist")); err == nil {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}
