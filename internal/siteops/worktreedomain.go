package siteops

import (
	"fmt"
	"strings"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
)

// WorktreeDomain resolves a site plus an optional branch to the domain whose
// custom nginx override applies: the site's primary domain when branch is
// empty, or the worktree's subdomain otherwise. It is the single source of
// truth for "branch -> {branch}.{primary}", shared by the CLI and MCP so they
// can't drift from how the daemon derives worktree domains.
func WorktreeDomain(site *config.Site, branch string) (string, error) {
	if branch == "" {
		return site.PrimaryDomain(), nil
	}
	wts, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil {
		return "", fmt.Errorf("detecting worktrees: %w", err)
	}
	for _, wt := range wts {
		if wt.Branch == branch {
			return wt.Domain, nil
		}
	}
	return "", fmt.Errorf("no worktree for branch %q on %s", branch, site.Name)
}

// SiteForDomain resolves a domain to the site that owns it, accepting both a
// registered domain (primary/alias) and a worktree's subdomain. Worktree
// overrides are keyed by the worktree's full domain (custom.d/{wt}.conf), so
// the per-site nginx endpoints must recognise those too; the worktree is
// confirmed against live git detection so an arbitrary subdomain can't be used
// to reach outside custom.d.
func SiteForDomain(domain string) (*config.Site, error) {
	if site, err := config.FindSiteByDomain(domain); err == nil {
		return site, nil
	}
	reg, err := config.LoadSites()
	if err != nil {
		return nil, err
	}
	for i := range reg.Sites {
		s := &reg.Sites[i]
		primary := s.PrimaryDomain()
		if !strings.HasSuffix(domain, "."+primary) {
			continue
		}
		wts, err := gitpkg.DetectWorktrees(s.Path, primary)
		if err != nil {
			continue
		}
		for _, wt := range wts {
			if wt.Domain == domain {
				return s, nil
			}
		}
	}
	return nil, fmt.Errorf("site with domain %q not found", domain)
}
