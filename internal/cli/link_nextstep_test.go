package cli

import (
	"strings"
	"testing"
)

func TestLinkNextStep(t *testing.T) {
	t.Run("standalone link suggests setup, never init", func(t *testing.T) {
		hint, suggest := linkNextStep(false)
		if !suggest {
			t.Fatal("expected a next-step suggestion after a standalone link")
		}
		if !strings.Contains(hint, "lerd setup") {
			t.Errorf("expected the hint to point at lerd setup, got %q", hint)
		}
		if strings.Contains(hint, "lerd init") {
			t.Errorf("hint should not mention lerd init, got %q", hint)
		}
	})

	t.Run("suppressed when link runs inside setup/init", func(t *testing.T) {
		hint, suggest := linkNextStep(true)
		if suggest {
			t.Error("expected no suggestion when link runs inside setup/init")
		}
		if hint != "" {
			t.Errorf("expected an empty hint when suppressed, got %q", hint)
		}
	})
}

func TestLinkShouldRunWizard(t *testing.T) {
	// hasConfig, interactive, hasDomainArg, isWorktree
	cases := []struct {
		name                                          string
		hasConfig, interactive, domainArg, isWorktree bool
		want                                          bool
	}{
		{"fresh interactive bare link runs wizard", false, true, false, false, true},
		{"existing config links directly", true, true, false, false, false},
		{"non-interactive stays bare (park/CI/scripts)", false, false, false, false, false},
		{"explicit domain arg links directly", false, true, true, false, false},
		{"worktree inherits parent, no wizard", false, true, false, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := linkShouldRunWizard(c.hasConfig, c.interactive, c.domainArg, c.isWorktree); got != c.want {
				t.Errorf("linkShouldRunWizard(%v,%v,%v,%v) = %v, want %v",
					c.hasConfig, c.interactive, c.domainArg, c.isWorktree, got, c.want)
			}
		})
	}
}
