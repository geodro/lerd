package siteops

import (
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	gitpkg "github.com/geodro/lerd/internal/git"
)

// SyncEnvIfPrimaryChanged updates APP_URL and the VITE_REVERB_* keys in the
// site's project .env and rewrites APP_URL in each worktree .env to the new
// <branch>.<newPrimary> subdomain, but only when the primary domain has
// actually changed since oldPrimary. Returns the first non-nil error so
// callers can warn; a worktree-detection failure is treated as no worktrees
// rather than as a fatal error because the parent sync has already landed.
func SyncEnvIfPrimaryChanged(site *config.Site, oldPrimary string) error {
	newPrimary := site.PrimaryDomain()
	if newPrimary == oldPrimary {
		return nil
	}
	if err := envfile.SyncPrimaryDomain(site.Path, newPrimary, site.Secured); err != nil {
		return err
	}
	scheme := "http"
	if site.Secured {
		scheme = "https"
	}
	worktrees, err := gitpkg.DetectWorktrees(site.Path, oldPrimary)
	if err != nil {
		return nil
	}
	var firstErr error
	for _, wt := range worktrees {
		newWTDomain := wt.Branch + "." + newPrimary
		if err := envfile.UpdateAppURL(wt.Path, scheme, newWTDomain); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
