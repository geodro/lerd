package ui

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/idle"
)

// TestTick_pinnedSiteStillTicksWorktrees is the regression guard for a pinned
// site stranding its worktrees. Pinning used to `continue` past tickWorktrees, so
// a pinned site's worktree was never re-detected: its domain dropped out of the
// access-feed lookup (no wake) and a suspended worktree was never resumed. The
// tick must still process the worktree, proven here by its domain landing in the
// engine's worktreeKeyByDomain map.
func TestTick_pinnedSiteStillTicksWorktrees(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := config.AddSite(config.Site{
		Name: "myapp", Path: "/srv/myapp", PHPVersion: "8.4",
		Domains: []string{"myapp.test"}, Pinned: true,
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}

	prev := detectWorktrees
	detectWorktrees = func(string, string) ([]gitpkg.Worktree, error) {
		return []gitpkg.Worktree{{
			Branch: "feature-x", Path: "/srv/myapp/feature-x", Domain: "feature-x.myapp.test",
		}}, nil
	}
	t.Cleanup(func() { detectWorktrees = prev })

	e := newIdleEngine(idle.NewTracker(nil))
	e.tick()

	key := wtKey("myapp", config.WorktreeUnitSlug("feature-x"))
	if got := e.worktreeKeyByDomain["feature-x.myapp.test"]; got != key {
		t.Errorf("pinned site's worktree domain = %q, want %q (worktree was skipped)", got, key)
	}
}

func TestWtKeyRoundTrip(t *testing.T) {
	key := wtKey("myapp", "feature-x")
	if key != "myapp/feature-x" {
		t.Fatalf("wtKey = %q, want myapp/feature-x", key)
	}
	site, wtBase, isWt := splitWtKey(key)
	if !isWt || site != "myapp" || wtBase != "feature-x" {
		t.Errorf("splitWtKey(%q) = (%q, %q, %v), want (myapp, feature-x, true)", key, site, wtBase, isWt)
	}
}

func TestSplitWtKey_mainSite(t *testing.T) {
	site, wtBase, isWt := splitWtKey("myapp")
	if isWt || site != "myapp" || wtBase != "" {
		t.Errorf("splitWtKey(myapp) = (%q, %q, %v), want (myapp, \"\", false)", site, wtBase, isWt)
	}
}
