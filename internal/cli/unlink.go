package cli

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteops"
	"github.com/spf13/cobra"
)

// NewUnlinkCmd returns the unlink command.
func NewUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink",
		Short: "Unlink the current directory site",
		Args:  cobra.NoArgs,
		RunE:  runUnlink,
	}
}

func runUnlink(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return fmt.Errorf("no site registered for %s — link it first with lerd link", cwd)
	}
	return UnlinkSite(site.Name)
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

	cfg, _ := config.LoadGlobal()
	var parkedDirs []string
	if cfg != nil {
		parkedDirs = cfg.ParkedDirectories
	}

	if err := siteops.UnlinkSiteCore(site, parkedDirs); err != nil {
		return err
	}

	fmt.Printf("Unlinked: %s (%s)\n", name, site.PrimaryDomain())

	// Offer to remove the cached custom container image.
	if site.IsCustomContainer() && podman.CustomImageExists(site.Name) {
		if isInteractive() {
			fmt.Print("Remove the container image? [y/N] ")
			var answer string
			fmt.Scanln(&answer) //nolint:errcheck
			if answer != "" && (answer[0] == 'y' || answer[0] == 'Y') {
				_ = podman.RemoveCustomImage(site.Name)
				fmt.Println("Image removed.")
			}
		}
	}

	autoStopUnusedServices()
	autoStopUnusedFPMs()

	return nil
}
