package cli

import (
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
)

// splitWorkerUnit parses the body of a worker unit name (i.e. without the
// leading "lerd-") into (kind, siteName, wtBase). It accepts both shapes
// the worker writers produce:
//
//   - parent: <kind>-<siteName>                          → wtBase = ""
//   - worktree: <kind>-<siteName>-<wtBase>               → wtBase = "<wtBase>"
//
// kinds with embedded hyphens (e.g. "custom-worker") are supported by
// matching the registered site name as the anchor. wtBase is verified
// against the parent's live worktrees so a registered site whose name
// happens to share a suffix with a worktree directory can't false-match.
//
// Lives in a non-tagged file (originally darwin-only) so the parsing logic
// is testable on Linux CI; only the migration *machinery* is darwin-only.
func splitWorkerUnit(rest string) (kind, siteName, wtBase string, ok bool) {
	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return "", "", "", false
	}
	// One pass over sites: parent suffix (longest wins) and internal-
	// token-with-worktree shape are checked in the same loop. Tracking
	// `bestParent` lets the longest registered name win without a second
	// scan, e.g. lerd-vite-alpha-beta resolves to site "alpha-beta" even
	// when "alpha" is also registered.
	bestParent := ""
	for _, s := range reg.Sites {
		if strings.HasSuffix(rest, "-"+s.Name) && len(s.Name) > len(bestParent) {
			bestParent = s.Name
		}
	}
	if bestParent != "" {
		if k := strings.TrimSuffix(rest, "-"+bestParent); k != "" {
			return k, bestParent, "", true
		}
	}
	for _, s := range reg.Sites {
		marker := "-" + s.Name + "-"
		idx := strings.Index(rest, marker)
		if idx <= 0 {
			continue
		}
		after := rest[idx+len(marker):]
		if after == "" {
			continue
		}
		k := rest[:idx]
		// Confirm `after` matches an actual worktree dir basename. If
		// DetectWorktrees errors (e.g. .git/worktrees gone) accept the
		// parse — better to retry the unit at parent path than to leave
		// it stuck.
		wts, derr := gitpkg.DetectWorktrees(s.Path, s.PrimaryDomain())
		if derr != nil {
			return k, s.Name, after, true
		}
		for _, wt := range wts {
			if filepath.Base(wt.Path) == after {
				return k, s.Name, after, true
			}
		}
	}
	return "", "", "", false
}
