package cli

import (
	"strings"
	"testing"
)

func TestErrNotLinkedMentionsLink(t *testing.T) {
	msg := errNotLinked().Error()
	if !strings.Contains(msg, "run 'lerd link' first") {
		t.Errorf("errNotLinked message changed, callers rely on it: %q", msg)
	}
}

// In a test process stdin is not a terminal, so ensureSiteForCwd must take the
// non-interactive branch and return the consistent error without prompting.
// The package source dir it runs in is not a registered site.
func TestEnsureSiteForCwdNonInteractiveErrors(t *testing.T) {
	_, err := ensureSiteForCwd()
	if err == nil {
		t.Fatal("expected an error for an unlinked directory in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "lerd link") {
		t.Errorf("error should point the user at lerd link, got: %v", err)
	}
}
