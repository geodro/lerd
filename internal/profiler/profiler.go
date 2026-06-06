// Package profiler turns the SPX profiler on or off globally. Arming flips a
// config flag and regenerates every PHP-FPM site's nginx vhost so the
// SPX_ENABLED cookie is injected into each request; disarming reverses it.
// No FPM restart is involved, only an nginx reload.
package profiler

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/siteops"
)

// SpxUIURL is the standalone SPX profiler web UI, served by the
// profiler.localhost nginx vhost. The dashboard embeds the same UI same-origin
// under /_spx/; this URL opens it directly (lerd profile open, MCP status).
const SpxUIURL = "http://profiler.localhost/?SPX_UI_URI=/"

// nginxReloadFn is the nginx reload hook, swapped out in tests.
var nginxReloadFn = nginx.Reload

// Result reports the outcome of a SetProfiling call.
type Result struct {
	Enabled  bool `json:"enabled"`
	NoChange bool `json:"no_change"`
}

// SetProfiling turns the SPX profiler on or off globally. When on, every
// PHP-FPM site's requests are profiled. The change regenerates each FPM
// site's vhost and reloads nginx.
func SetProfiling(on bool) (Result, error) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return Result{}, err
	}
	if cfg.IsProfilerEnabled() == on {
		return Result{Enabled: on, NoChange: true}, nil
	}
	cfg.Profiler.Enabled = on
	if err := config.SaveGlobal(cfg); err != nil {
		return Result{}, fmt.Errorf("saving config: %w", err)
	}
	if err := regenerateVhosts(); err != nil {
		return Result{}, err
	}
	if err := nginxReloadFn(); err != nil {
		return Result{}, fmt.Errorf("reloading nginx: %w", err)
	}
	return Result{Enabled: on}, nil
}

// ClearData deletes every captured SPX report from the profiler data
// directory. The directory itself is kept so the read-write bind mount into
// each FPM container stays valid; only its contents go. A missing or empty
// directory is not an error. Returns how many top-level entries were removed.
func ClearData() (int, error) {
	dir := config.SpxDataDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading spx data dir: %w", err)
	}
	removed := 0
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return removed, fmt.Errorf("removing %s: %w", e.Name(), err)
		}
		removed++
	}
	return removed, nil
}

// regenerateVhosts rewrites the vhost of every active PHP-FPM site so the
// SPX_ENABLED injection reflects the current toggle. Paused, ignored,
// custom-container and FrankenPHP sites are skipped: they have no FPM vhost
// to profile, and regenerating a paused site would revive it.
func regenerateVhosts() error {
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	for i := range reg.Sites {
		s := reg.Sites[i]
		if s.Ignored || s.Paused || s.IsCustomContainer() || s.IsFrankenPHP() || s.IsHostProxy() {
			continue
		}
		if err := siteops.RegenerateSiteVhost(&s, s.PrimaryDomain()); err != nil {
			return fmt.Errorf("regenerating vhost for %s: %w", s.Name, err)
		}
		// Worktree vhosts share the site template, so they need the toggle too.
		worktrees, err := gitpkg.DetectWorktrees(s.Path, s.PrimaryDomain())
		if err != nil {
			continue
		}
		for _, wt := range worktrees {
			php := config.WorktreePHPVersion(wt.Path, s.PHPVersion)
			_ = nginx.GenerateWorktreeVhostFor(wt.Domain, wt.Path, php, s.PrimaryDomain(), s.Name, wt.Branch, s.Secured)
		}
	}
	return nil
}
