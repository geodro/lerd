package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	gitpkg "github.com/geodro/lerd/internal/git"
)

// DropOrphanedWorktreeDBs scans the registry for orphaned worktree state
// (LAN shares first, isolated databases last) and tears it down. Database
// drop is intentionally the LAST step so any earlier failure (LAN proxy
// stop, daemon notify, registry write) leaves the data intact and the user
// can recover by re-adding the worktree without losing migrations or seed
// data.
func DropOrphanedWorktreeDBs(site *config.Site) {
	live := liveWorktreeBranches(site)
	DropOrphanedWorktreeLANShares(site, live)
	dbEntries, _ := config.WorktreeDBsForSite(site.Name)
	for _, e := range dbEntries {
		if live[e.Branch] {
			continue
		}
		_, _ = DropDatabase(e.Service, e.DBName)
		_, _, _ = config.RemoveWorktreeDB(e.Site, e.Branch)
	}
}

func liveWorktreeBranches(site *config.Site) map[string]bool {
	out := map[string]bool{}
	worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil {
		return out
	}
	for _, w := range worktrees {
		out[w.Branch] = true
	}
	return out
}

// SetWorktreeDBIsolated is the shared lifecycle helper used by both the HTTP
// handler and the `lerd db:isolate` / `lerd db:share` CLI commands. On enable
// it creates `<parent_db>_<sanitized_branch>` in the same service the parent
// uses, optionally clones from `source` (empty / "main" / another isolated
// branch), records the worktree-DB pair in the registry, and rewrites
// DB_DATABASE in the worktree's .env. On disable it drops the DB and restores
// the parent's value. Idempotent.
func SetWorktreeDBIsolated(site *config.Site, branch string, isolated bool, source string) error {
	worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil {
		return fmt.Errorf("detecting worktrees: %w", err)
	}
	var wt gitpkg.Worktree
	found := false
	for _, w := range worktrees {
		if w.Branch == branch {
			wt = w
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown worktree branch %q", branch)
	}

	parentEnv := filepath.Join(site.Path, ".env")
	parentDB := envfile.ReadKey(parentEnv, "DB_DATABASE")
	parentHost := envfile.ReadKey(parentEnv, "DB_HOST")
	if parentDB == "" || !strings.HasPrefix(parentHost, "lerd-") {
		return fmt.Errorf("parent site does not use a lerd-managed mysql/postgres (DB_HOST=%q, DB_DATABASE=%q)", parentHost, parentDB)
	}
	service := strings.TrimPrefix(parentHost, "lerd-")
	dbName := WorktreeDBName(parentDB, branch)

	if isolated {
		if _, err := CreateDatabase(service, dbName); err != nil {
			return fmt.Errorf("creating database %q in %s: %w", dbName, service, err)
		}
		if cloneSrc := resolveCloneSource(site, branch, source, parentDB); cloneSrc != "" {
			if err := CloneDatabase(service, cloneSrc, dbName); err != nil {
				_, _ = DropDatabase(service, dbName)
				return err
			}
		}
		if err := config.AddWorktreeDB(config.WorktreeDBEntry{
			Site:    site.Name,
			Branch:  branch,
			Service: service,
			DBName:  dbName,
		}); err != nil {
			return fmt.Errorf("recording worktree db: %w", err)
		}
		if err := config.SetWorktreeDBIsolated(wt.Path, true); err != nil {
			return fmt.Errorf("updating .lerd.yaml: %w", err)
		}
		if err := envfile.ApplyUpdates(filepath.Join(wt.Path, ".env"), map[string]string{
			"DB_DATABASE": dbName,
		}); err != nil {
			return fmt.Errorf("rewriting worktree .env: %w", err)
		}
		return nil
	}

	if entry, removed, err := config.RemoveWorktreeDB(site.Name, branch); err == nil && removed {
		_, _ = DropDatabase(entry.Service, entry.DBName)
	}
	if err := config.SetWorktreeDBIsolated(wt.Path, false); err != nil {
		return fmt.Errorf("updating .lerd.yaml: %w", err)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, ".env")); err == nil {
		if err := envfile.ApplyUpdates(filepath.Join(wt.Path, ".env"), map[string]string{
			"DB_DATABASE": parentDB,
		}); err != nil {
			return fmt.Errorf("restoring worktree .env: %w", err)
		}
	}
	return nil
}

// WorktreeDBName mirrors the projectDBName convention so a parent named
// "acme_app" with branch "feat-x" becomes "acme_app_feat_x".
func WorktreeDBName(parentDB, branch string) string {
	return parentDB + "_" + config.SiteSlug(branch)
}

func resolveCloneSource(site *config.Site, branch, source, parentDB string) string {
	switch source {
	case "", "empty":
		return ""
	case "main":
		return parentDB
	default:
		if entry, ok, err := config.FindWorktreeDB(site.Name, source); err == nil && ok && source != branch {
			return entry.DBName
		}
		return ""
	}
}

// FindParentSiteForWorktree looks up the registered site whose worktrees
// contain dir. Returns (site, branch, true) on a match, otherwise (_, _, false).
func FindParentSiteForWorktree(dir string) (*config.Site, string, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", false
	}
	reg, err := config.LoadSites()
	if err != nil {
		return nil, "", false
	}
	for i := range reg.Sites {
		s := &reg.Sites[i]
		if s.Ignored {
			continue
		}
		worktrees, err := gitpkg.DetectWorktrees(s.Path, s.PrimaryDomain())
		if err != nil {
			continue
		}
		for _, wt := range worktrees {
			if wt.Path == abs {
				return s, wt.Branch, true
			}
		}
	}
	return nil, "", false
}
