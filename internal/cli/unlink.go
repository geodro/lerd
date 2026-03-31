package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/spf13/cobra"
)

// NewUnlinkCmd returns the unlink command.
func NewUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink [name]",
		Short: "Unlink the current directory site",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runUnlink,
	}
}

func runUnlink(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return UnlinkSite(args[0])
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	// Look up by path so directory names like "canavanbyrne.ie" resolve
	// correctly to their registered site name (e.g. "canavanbyrne-ie").
	if site, err := config.FindSiteByPath(cwd); err == nil {
		return UnlinkSite(site.Name)
	}
	return UnlinkSite(filepath.Base(cwd))
}

// UnlinkSite removes the nginx vhost for the named site. For sites under a parked
// directory, the registry entry is kept but marked Ignored so the watcher does not
// re-register it. For manually-linked sites the entry is removed entirely.
func UnlinkSite(name string) error {
	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found", name)
	}

	for _, w := range collectRunningWorkers(site) {
		stopWorkerByName(site, w)
	}

	if err := nginx.RemoveVhost(site.Domain); err != nil {
		fmt.Printf("[WARN] removing vhost: %v\n", err)
	}

	cfg, _ := config.LoadGlobal()
	isParked := false
	if cfg != nil {
		for _, dir := range cfg.ParkedDirectories {
			if filepath.Dir(site.Path) == dir {
				isParked = true
				break
			}
		}
	}

	if isParked {
		if err := config.IgnoreSite(name); err != nil {
			return fmt.Errorf("ignoring site: %w", err)
		}
	} else {
		if err := config.RemoveSite(name); err != nil {
			return fmt.Errorf("removing site from registry: %w", err)
		}
	}

	fmt.Printf("Unlinked: %s (%s)\n", name, site.Domain)

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}

	autoStopUnusedServices()

	return nil
}
