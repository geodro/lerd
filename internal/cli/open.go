package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
)

// NewOpenCmd returns the open command.
func NewOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open [site]",
		Short: "Open the current site in the default browser",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runOpen,
	}
}

func runOpen(_ *cobra.Command, args []string) error {
	var url string

	if len(args) > 0 {
		// Name provided — look it up directly.
		site, err := config.FindSite(args[0])
		if err != nil {
			return fmt.Errorf("site %q not found", args[0])
		}
		url = siteURL(site.Path)
	} else {
		// No argument — use cwd.
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		url = siteURL(cwd)
		if url == "" {
			// Fall back: maybe cwd is named like a site.
			name, _ := siteNameAndDomain(filepath.Base(cwd), "test")
			if site, err := config.FindSite(name); err == nil {
				url = siteURL(site.Path)
			}
		}
	}

	if url == "" {
		return fmt.Errorf("no registered site found for this directory — run 'lerd link' first")
	}

	fmt.Printf("Opening %s\n", url)
	return openBrowser(url)
}
