package siteops

import (
	"fmt"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	nodeDet "github.com/geodro/lerd/internal/node"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
)

// DetectSiteVersions resolves the framework, PHP version (clamped to framework
// range), and Node version for a project directory. This is the shared detection
// logic used by link, park registration, and MCP site_link.
func DetectSiteVersions(dir, framework, defaultPHP, defaultNode string) (phpVersion, nodeVersion string) {
	phpMin, phpMax := "", ""
	if framework != "" {
		if fw, ok := config.GetFrameworkForDir(framework, dir); ok {
			phpMin, phpMax = fw.PHP.Min, fw.PHP.Max
		}
	}

	phpVersion = phpDet.DetectVersionClamped(dir, phpMin, phpMax, defaultPHP)

	nodeVersion, err := nodeDet.DetectVersion(dir)
	if err != nil {
		nodeVersion = defaultNode
	}

	return phpVersion, nodeVersion
}

// CleanupRelink handles the re-link scenario: when a site is being linked at a
// path that already has registrations, it carries over the secured state and
// removes stale entries (e.g. name changed). Returns the carried-over secured flag.
func CleanupRelink(path, newName string) bool {
	secured := false
	reg, err := config.LoadSites()
	if err != nil {
		return false
	}
	for _, existing := range reg.Sites {
		if existing.Path != path {
			continue
		}
		secured = secured || existing.Secured
		if existing.Name != newName {
			_ = nginx.RemoveVhost(existing.PrimaryDomain())
			_ = config.RemoveSite(existing.Name)
		}
	}
	return secured
}

// FinishLink performs the post-registration steps shared by link, park, and MCP:
// vhost generation, FPM quadlet setup, container hosts update, and nginx reload.
func FinishLink(site config.Site, phpVersion string) error {
	if site.Secured {
		if err := certs.SecureSite(site); err != nil {
			return fmt.Errorf("securing site: %w", err)
		}
	} else {
		if err := nginx.GenerateVhost(site, phpVersion); err != nil {
			return fmt.Errorf("generating vhost: %w", err)
		}
	}

	_ = podman.WriteXdebugIni(phpVersion, false)
	if err := podman.WriteFPMQuadlet(phpVersion); err == nil {
		_ = podman.DaemonReload()
	}

	_ = podman.RewriteFPMQuadlets()
	_ = podman.WriteContainerHosts()

	if err := nginx.Reload(); err != nil {
		return fmt.Errorf("nginx reload: %w", err)
	}

	return nil
}
