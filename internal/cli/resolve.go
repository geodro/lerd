package cli

import (
	"fmt"

	"github.com/geodro/lerd/internal/config"
)

// errNotLinked is the single message every directory-scoped command shows when
// the current directory has no registered site, replacing several earlier
// phrasings so the guidance is consistent everywhere.
func errNotLinked() error {
	return fmt.Errorf("no site registered for this directory — run 'lerd link' first")
}

// ensureSiteForCwd resolves the site registered for cwd. When none is found in
// an interactive terminal it offers to link the directory, which cascades into
// lerd init, then re-resolves so the caller proceeds in one flow.
func ensureSiteForCwd(cwd string) (*config.Site, error) {
	if site, err := config.FindSiteByPath(cwd); err == nil {
		return site, nil
	}
	if !isInteractive() {
		return nil, errNotLinked()
	}

	fmt.Print("This directory isn't linked to lerd. Link it now? [Y/n] ")
	var answer string
	fmt.Scanln(&answer) //nolint:errcheck
	if !(answer == "" || answer[0] == 'Y' || answer[0] == 'y') {
		return nil, errNotLinked()
	}

	if err := runLink(nil); err != nil {
		return nil, err
	}
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return nil, errNotLinked()
	}
	return site, nil
}
