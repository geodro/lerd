package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
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

	// Remove vhost — the conf file is named after the primary domain and
	// contains all domains in server_name, so one removal covers everything.
	if err := nginx.RemoveVhost(site.PrimaryDomain()); err != nil {
		fmt.Printf("[WARN] removing vhost: %v\n", err)
	}

	// Remove certificates if the site was secured.
	if site.Secured {
		certsDir := filepath.Join(config.CertsDir(), "sites")
		os.Remove(filepath.Join(certsDir, site.PrimaryDomain()+".crt")) //nolint:errcheck
		os.Remove(filepath.Join(certsDir, site.PrimaryDomain()+".key")) //nolint:errcheck
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

	fmt.Printf("Unlinked: %s (%s)\n", name, site.PrimaryDomain())

	if err := nginx.Reload(); err != nil {
		fmt.Printf("[WARN] nginx reload: %v\n", err)
	}

	if err := podman.WriteContainerHosts(); err != nil {
		fmt.Printf("[WARN] updating container hosts file: %v\n", err)
	}

	autoStopUnusedServices()
	autoStopUnusedFPMs()

	// Rewrite FPM quadlets to remove volume mounts that are no longer needed.
	_ = podman.RewriteFPMQuadlets()

	return nil
}
