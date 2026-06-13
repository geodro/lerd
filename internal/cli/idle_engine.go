package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
)

// SuspendWorkersForIdle stops ALL of the site's running workers (queue, Horizon,
// scheduler, Stripe, vite, ...) and returns the names it stopped, for the caller
// to persist as Site.IdleSuspendedWorkers. An idle site does no background work.
//
// Vite is the one special case: stopping it makes Laravel's @vite directive fall
// back to the built asset manifest, so a site with no build would serve a broken
// page. So before sleeping we ensure a usable build exists (running `npm run
// build` if missing) and clear public/hot; if no build can be produced, vite is
// left running for that site. Idempotent.
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
		stopWorkerByName(site, w)
		suspended = append(suspended, w)
	}
	return suspended
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
		runViteBuildAt(site, dir)
	}
	if !viteManifestExists(dir, pub) {
		return false
	}
	_ = os.Remove(filepath.Join(dir, pub, "hot"))
	return true
}

// runViteBuild runs `npm run build` for the site on the host via its pinned Node
// version, the same way the vite host worker runs `npm run dev`. Blocking; the
// caller runs it off the engine's hot path.
func runViteBuild(site *config.Site) { runViteBuildAt(site, site.Path) }

// runViteBuildAt runs `npm run build` in dir (a site or worktree checkout).
func runViteBuildAt(site *config.Site, dir string) {
	nodeVersion := site.NodeVersion
	if nodeVersion == "" {
		nodeVersion = "default"
	}
	fnm := filepath.Join(config.BinDir(), "fnm")
	cmd := exec.Command(fnm, "exec", "--using="+nodeVersion, "--", "npm", "run", "build")
	cmd.Dir = dir
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

func lastBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[len(b)-n:]
}
