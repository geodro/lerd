package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/siteinfo"
)

// unitStatesFn snapshots every lerd-* unit's state; a var so tests can pin which
// units exist. A worker absent from it has had its unit removed entirely, which
// is idle-suspend's signature (it removes units, not just stops them).
var unitStatesFn = siteinfo.AllUnitStates

// SuspendWorkersForIdle stops ALL of the site's running workers (queue, Horizon,
// scheduler, Stripe, vite, ...) and returns the names to persist as
// Site.IdleSuspendedWorkers. An idle site does no background work.
//
// Vite is the one special case: stopping it makes Laravel's @vite directive fall
// back to the built asset manifest, so a site with no build would serve a broken
// page. So before sleeping we ensure a usable build exists (running `npm run
// build` if missing) and clear public/hot; if no build can be produced, vite is
// left running for that site. Idempotent.
//
// The returned set also includes declared workers whose unit was removed entirely
// (see appendLostSuspended), so an idle site whose persisted suspended list was
// wiped while its units stayed gone (e.g. a reinstall) is re-marked as sleepy
// rather than left showing as "off" with no way for the engine to recover it.
func SuspendWorkersForIdle(site *config.Site) []string {
	running := collectRunningWorkers(site)

	// Resolve vite up front (it may run a build), before stopping anything, so
	// the site stays fully up during the one-time build.
	viteSleepable := true
	if containsString(running, "vite") {
		viteSleepable = ensureViteSleepable(site)
	}

	var suspended []string
	for _, w := range running {
		if w == "vite" && !viteSleepable {
			continue // no usable build; keep vite running so the page isn't broken
		}
		if !idleWorkerResumable(site, w) {
			// An orphaned unit (no framework definition) can't be brought back by
			// ResumeWorkersForIdle, so leave it running rather than strand it
			// suspended forever. collectRunningWorkers includes such orphans.
			continue
		}
		stopWorkerByName(site, w)
		suspended = append(suspended, w)
	}
	return appendLostSuspended(site, suspended)
}

// appendLostSuspended adds any of the site's declared workers (.lerd.yaml) whose
// systemd unit is gone entirely and that are resumable. A removed unit is
// idle-suspend's own signature, so such a worker is one whose persisted
// "suspended" marking was lost (e.g. a reinstall cleared the list) while its unit
// stayed gone; re-listing it restores the sleeping state instead of leaving it
// showing as "off". A worker that merely crashed or stopped still has its unit
// (active/failed/inactive), so it is left to worker-healing; one the user stopped
// or removed is dropped from .lerd.yaml entirely, so neither is ever re-marked.
func appendLostSuspended(site *config.Site, suspended []string) []string {
	proj, err := config.LoadProjectConfig(site.Path)
	if err != nil || proj == nil {
		return suspended
	}
	states := unitStatesFn()
	for _, w := range proj.Workers {
		if containsString(suspended, w) {
			continue
		}
		if _, unitExists := states["lerd-"+w+"-"+site.Name]; unitExists {
			continue // unit still present -> not idle-suspend's doing, leave it alone
		}
		if idleWorkerResumable(site, w) {
			suspended = append(suspended, w)
		}
	}
	return suspended
}

// idleWorkerResumable reports whether ResumeWorkersForIdle (resumeWorkerByName)
// can bring a worker back. Idle-suspend must never stop a worker it can't
// restart, or the worker is stranded suspended forever. Mirrors the branches of
// resumeWorkerByName so the two stay in lockstep.
func idleWorkerResumable(site *config.Site, workerName string) bool {
	switch workerName {
	case "stripe":
		return true
	case hostProxyWorkerName:
		// resumeWorkerByName only restarts the host-proxy worker when the project
		// still declares a proxy command; without this guard a site whose proxy
		// block was removed would be suspended but never resumed.
		proj, _ := config.LoadProjectConfig(site.Path)
		return proj != nil && proj.Proxy != nil
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok || fw.Workers == nil {
		return false
	}
	_, ok = fw.Workers[workerName]
	return ok
}

// ResumeWorkersForIdle restarts workers previously suspended by idle-suspend.
// Idempotent: starting an already-running worker is harmless, which lets the
// engine self-heal stale suspended state after a `lerd start` restarted them.
func ResumeWorkersForIdle(site *config.Site, workers []string) {
	phpVersion := site.PHPVersion
	if detected, err := phpDet.DetectVersion(site.Path); err == nil && detected != "" {
		phpVersion = detected
	}
	for _, w := range workers {
		resumeWorkerByName(site, w, phpVersion)
	}
}

// SuspendWorktreeWorkersForIdle stops a git worktree's own per-worktree workers
// (for Laravel that's just vite) by their worktree unit names and returns the
// names stopped, for the caller to persist. Mirrors SuspendWorkersForIdle but
// targets lerd-<w>-<site>-<wtBase> units and runs the vite build in the worktree
// checkout so a sleeping worktree serves built assets. wtPath is the worktree's
// checkout directory.
func SuspendWorktreeWorkersForIdle(site *config.Site, wtPath string) []string {
	running := collectRunningWorktreeWorkers(site, wtPath)

	viteSleepable := true
	if containsString(running, "vite") {
		viteSleepable = ensureViteSleepableAt(site, wtPath)
	}

	var suspended []string
	for _, w := range running {
		if w == "vite" && !viteSleepable {
			continue
		}
		WorkerStopForSite(site.Name, wtPath, w) //nolint:errcheck
		suspended = append(suspended, w)
	}
	return suspended
}

// ResumeWorktreeWorkersForIdle restarts a worktree's previously suspended
// workers. Idempotent, like ResumeWorkersForIdle.
func ResumeWorktreeWorkersForIdle(site *config.Site, wtPath string, workers []string) {
	phpVersion := site.PHPVersion
	if detected, err := phpDet.DetectVersion(wtPath); err == nil && detected != "" {
		phpVersion = detected
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok || fw.Workers == nil {
		return
	}
	for _, w := range workers {
		worker, ok := fw.Workers[w]
		if !ok {
			continue
		}
		WorkerStartForSite(site.Name, wtPath, phpVersion, w, worker, true) //nolint:errcheck
	}
}

// IdleSuspendStateIsStale reports whether a site's persisted idle-suspended set
// has drifted from reality: a worker it claims to have suspended is actually
// running. That happens when the workers were (re)started outside the idle engine
// by an install or relink with an older lerd that didn't reconcile the list. Left
// uncorrected it wedges the engine into believing the site is asleep forever, so
// its idle workers never get re-suspended. The engine calls this at startup to
// discard a stale list rather than seed itself from it.
func IdleSuspendStateIsStale(site *config.Site) bool {
	if len(site.IdleSuspendedWorkers) == 0 {
		return false
	}
	running := collectRunningWorkers(site)
	for _, w := range site.IdleSuspendedWorkers {
		if containsString(running, w) {
			return true
		}
	}
	return false
}

// WorktreeIdleSuspendStateIsStale is IdleSuspendStateIsStale for a single git
// worktree: it reports whether the worktree's persisted idle-suspended set has
// drifted from reality (a worker it claims suspended is actually running, e.g.
// an install/relink restarted it without clearing the slot). The engine calls it
// at startup so a stale worktree list is discarded rather than seeded from, which
// would wedge the worktree as suspended forever and never re-suspend it.
func WorktreeIdleSuspendStateIsStale(site *config.Site, wtBase string, suspended []string) bool {
	if len(suspended) == 0 {
		return false
	}
	running := collectRunningWorktreeWorkersByBase(site, wtBase)
	for _, w := range suspended {
		if containsString(running, w) {
			return true
		}
	}
	return false
}

// ensureViteSleepable makes a site safe to stop vite on. Vite needs a built
// asset manifest to fall back to once its dev server stops; if one is missing it
// runs `npm run build` (blocking) and reports whether a manifest now exists.
// When sleepable it clears public/hot so @vite uses the manifest, not the
// stopped dev server. A build that fails leaves vite running, so a sleeping site
// never serves a broken page.
//
// The build runs at most once per checkout, never on every suspend: a manifest
// persists across dev runs, so once one exists later suspends reuse it. Don't
// "freshen" it by rebuilding each suspend — that would thrash a flapping site
// for no benefit, and a sleeping site serving slightly stale assets is fine
// (editing it wakes it, and an edit rebuilds via the dev server anyway).
func ensureViteSleepable(site *config.Site) bool {
	return ensureViteSleepableAt(site, site.Path)
}

// ensureViteSleepableAt is ensureViteSleepable for an arbitrary checkout dir, so
// a git worktree's own vite can sleep against a build in the worktree path rather
// than the main site's.
func ensureViteSleepableAt(site *config.Site, dir string) bool {
	pub := sitePublicDir(site)
	if !viteManifestExists(dir, pub) {
		// Build at most once per checkout per session. A worktree often has no
		// built manifest and its `npm run build` may fail (deps not installed);
		// without this guard the engine would re-run the build on every 30s tick
		// for such a checkout, since a failed build leaves it unsuspended.
		if _, tried := viteBuildAttempted.LoadOrStore(dir, struct{}{}); !tried {
			runViteBuildAt(site, dir)
		}
	}
	if !viteManifestExists(dir, pub) {
		return false
	}
	_ = os.Remove(filepath.Join(dir, pub, "hot"))
	return true
}

// viteBuildAttempted remembers the checkouts the idle engine has already tried a
// one-time `npm run build` for this session, so a failing build is not retried
// every tick. A later successful dev run drops a manifest that ensureViteSleepableAt
// picks up regardless of this set.
var viteBuildAttempted sync.Map

// runViteBuildAt runs `npm run build` in dir (a site or worktree checkout). A
// var so tests can stand in for the host build without invoking fnm/npm.
var runViteBuildAt = func(site *config.Site, dir string) {
	nodeVersion := site.NodeVersion
	if nodeVersion == "" {
		nodeVersion = "default"
	}
	fnm := filepath.Join(config.BinDir(), "fnm")
	cmd := exec.Command(fnm, "exec", "--using="+nodeVersion, "--", "npm", "run", "build")
	cmd.Dir = dir
	cmd.Env = shimLeadingEnv(os.Environ()) // wayfinder needs lerd's php shim to lead PATH — issue #381
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("[idle] %s: `npm run build` failed, keeping vite running: %v\n%s\n",
			site.Name, err, lastBytes(out, 600))
	}
}

// sitePublicDir is the site's public document root relative to its project root.
func sitePublicDir(site *config.Site) string {
	if site.PublicDir != "" {
		return site.PublicDir
	}
	return "public"
}

// viteManifestExists reports whether a built Vite manifest is present, covering
// both the Vite 5+ location and the older .vite/ one.
func viteManifestExists(sitePath, pub string) bool {
	for _, p := range []string{
		filepath.Join(sitePath, pub, "build", "manifest.json"),
		filepath.Join(sitePath, pub, "build", ".vite", "manifest.json"),
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// removeWorker returns ss without the first occurrence of w plus whether it was
// present, preserving the order of the rest. The full-slice expression forces a
// fresh backing array so the caller's slice isn't mutated underneath it.
func removeWorker(ss []string, w string) ([]string, bool) {
	for i, s := range ss {
		if s == w {
			return append(ss[:i:i], ss[i+1:]...), true
		}
	}
	return ss, false
}

// ClearIdleSuspendOnStart drops workerName from the site's (or, for a worktree
// checkout, that worktree's) persisted idle-suspended set whenever the worker is
// (re)started outside the idle engine: an install, a relink, or `lerd worker
// start`. A running worker can't be idle-suspended, so a stale entry would make
// the engine boot believing the site is asleep and never re-suspend it. Cheap
// no-op (one read, no write) when the worker isn't in the set, which is the
// common case for the vast majority of starts.
func ClearIdleSuspendOnStart(siteName, sitePath, workerName string) {
	site, err := config.FindSite(siteName)
	if err != nil {
		return
	}
	if sitePath != "" && sitePath != site.Path {
		wtBase := config.WorktreeUnitSlug(filepath.Base(sitePath))
		if next, changed := removeWorker(site.WorktreeIdleSuspended[wtBase], workerName); changed {
			_ = config.SetWorktreeIdleSuspendedWorkers(siteName, wtBase, next)
		}
		return
	}
	if next, changed := removeWorker(site.IdleSuspendedWorkers, workerName); changed {
		_ = config.SetSiteIdleSuspendedWorkers(siteName, next)
	}
}

func lastBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[len(b)-n:]
}
