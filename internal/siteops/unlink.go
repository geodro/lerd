package siteops

import (
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
)

// IsParkedSite checks whether a site's path is inside one of the parked directories.
func IsParkedSite(sitePath string, parkedDirs []string) bool {
	parent := filepath.Dir(sitePath)
	for _, dir := range parkedDirs {
		expanded := os.ExpandEnv(dir)
		if home, err := os.UserHomeDir(); err == nil {
			if len(expanded) > 0 && expanded[0] == '~' {
				expanded = filepath.Join(home, expanded[1:])
			}
		}
		if parent == expanded {
			return true
		}
	}
	return false
}

// StopSiteWorkers, when set, stops all running workers for a site as part of
// UnlinkSiteCore. It is wired up by the cli package (which owns worker
// lifecycle) at init time, mirroring podman.AfterUnitChange. Without it, the
// MCP and parked-watcher unlink paths — which call UnlinkSiteCore directly —
// would leave a host-proxy site's always-restart dev-server worker (and any
// framework workers) running after the site is gone.
var StopSiteWorkers func(site *config.Site)

// UnlinkSiteCore performs the shared unlink steps: stop workers, remove vhost,
// remove certs, update registry (ignore if parked, remove otherwise), update
// container hosts, and reload nginx.
func UnlinkSiteCore(site *config.Site, parkedDirs []string) error {
	if StopSiteWorkers != nil {
		StopSiteWorkers(site)
	}

	_ = nginx.RemoveVhost(site.PrimaryDomain())

	if site.Secured {
		certsDir := config.CertsDir()
		domain := site.PrimaryDomain()
		os.Remove(filepath.Join(certsDir, domain+".crt")) //nolint:errcheck
		os.Remove(filepath.Join(certsDir, domain+".key")) //nolint:errcheck
	}

	// Clean up the per-project custom container if this site uses one.
	// The image is kept so relinking is fast; use `lerd rebuild` to
	// force a fresh build.
	if site.IsCustomContainer() {
		_ = podman.StopUnit(podman.CustomContainerName(site.Name))
		podman.RemoveCustomContainer(site.Name)
		_ = podman.RemoveCustomContainerQuadlet(site.Name)
	}

	// Same cleanup for FrankenPHP sites: stop and remove the per-site
	// quadlet. The dunglas/frankenphp image is shared across all FrankenPHP
	// sites on this PHP version, so it stays in the local store.
	if site.IsFrankenPHP() {
		_ = podman.StopUnit(podman.FrankenPHPContainerName(site.Name))
		_ = podman.RemoveFrankenPHPQuadlet(site.Name)
	}

	if IsParkedSite(site.Path, parkedDirs) {
		_ = config.IgnoreSite(site.Name)
	} else {
		_ = config.RemoveSite(site.Name)
	}

	_ = podman.WriteContainerHosts()
	_ = podman.RewriteFPMQuadlets()

	if err := nginx.Reload(); err != nil {
		return err
	}

	// See FinishLink: unlinking doesn't start/stop a systemd unit, so
	// the shared hook wouldn't otherwise fire. Notify explicitly.
	if podman.AfterUnitChange != nil {
		podman.AfterUnitChange("site:" + site.Name)
	}
	return nil
}
