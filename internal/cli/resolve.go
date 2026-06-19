package cli

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/config"
)

// errNotLinked is the single message every directory-scoped command shows when
// the current directory has no registered site, replacing several earlier
// phrasings so the guidance is consistent everywhere.
func errNotLinked() error {
	return fmt.Errorf("no site registered for this directory — run 'lerd link' first")
}

// ensureSiteForCwd resolves the site for the current working directory, using
// os.Getwd for both lookup and link so they can't diverge. On a miss in an
// interactive terminal it offers to link (cascading into init) and re-resolves.
func ensureSiteForCwd() (*config.Site, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
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
