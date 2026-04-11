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

// UnlinkSiteCore performs the shared unlink steps: remove vhost, remove certs,
// update registry (ignore if parked, remove otherwise), update container hosts,
// and reload nginx. It does NOT stop workers or clean up unused services/FPMs —
// callers that need those should do them before calling this function.
func UnlinkSiteCore(site *config.Site, parkedDirs []string) error {
	_ = nginx.RemoveVhost(site.PrimaryDomain())

	if site.Secured {
		certsDir := config.CertsDir()
		domain := site.PrimaryDomain()
		os.Remove(filepath.Join(certsDir, domain+".crt")) //nolint:errcheck
		os.Remove(filepath.Join(certsDir, domain+".key")) //nolint:errcheck
	}

	if IsParkedSite(site.Path, parkedDirs) {
		_ = config.IgnoreSite(site.Name)
	} else {
		_ = config.RemoveSite(site.Name)
	}

	_ = podman.WriteContainerHosts()
	_ = podman.RewriteFPMQuadlets()

	return nginx.Reload()
}
