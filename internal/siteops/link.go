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

// VersionResult holds the detected and suggested versions for a site.
type VersionResult struct {
	PHP          string // best installed PHP version (clamped to framework range)
	Node         string // detected Node version
	PHPMin       string // framework minimum PHP version (empty if no framework)
	PHPMax       string // framework maximum PHP version (empty if no framework)
	SuggestedPHP string // better PHP version to install (empty if current is optimal)
	FrameworkLabel string // human-readable framework name for messages
}

// DetectSiteVersions resolves the framework, PHP version (clamped to framework
// range), and Node version for a project directory. When the best installed PHP
// version is below the framework's max, SuggestedPHP is set to the max version.
func DetectSiteVersions(dir, framework, defaultPHP, defaultNode string) VersionResult {
	result := VersionResult{}

	if framework != "" {
		if fw, ok := config.GetFrameworkForDir(framework, dir); ok {
			result.PHPMin = fw.PHP.Min
			result.PHPMax = fw.PHP.Max
			result.FrameworkLabel = fw.Label
		}
	}

	result.PHP = phpDet.DetectVersionClamped(dir, result.PHPMin, result.PHPMax, defaultPHP)

	nodeVersion, err := nodeDet.DetectVersion(dir)
	if err != nil {
		nodeVersion = defaultNode
	}
	result.Node = nodeVersion

	// Suggest installing a better PHP version only when the detected system
	// version was ABOVE the framework's max (i.e. clamping had to downgrade)
	// and a higher version within range could be installed.
	if result.PHPMax != "" && result.PHPMin != "" {
		unclamped := phpDet.DetectVersionClamped(dir, "", "", defaultPHP)
		if compareMajorMinor(unclamped, result.PHPMax) > 0 && compareMajorMinor(result.PHP, result.PHPMax) < 0 {
			installed, _ := phpDet.ListInstalled()
			maxInstalled := false
			for _, v := range installed {
				if v == result.PHPMax {
					maxInstalled = true
					break
				}
			}
			if !maxInstalled {
				result.SuggestedPHP = result.PHPMax
			}
		}
	}

	return result
}

// compareMajorMinor compares two "major.minor" version strings.
// Returns -1 if a < b, 0 if equal, 1 if a > b.
func compareMajorMinor(a, b string) int {
	aMaj, aMin := parseMM(a)
	bMaj, bMin := parseMM(b)
	if aMaj != bMaj {
		if aMaj < bMaj {
			return -1
		}
		return 1
	}
	if aMin != bMin {
		if aMin < bMin {
			return -1
		}
		return 1
	}
	return 0
}

func parseMM(v string) (int, int) {
	var maj, min int
	fmt.Sscanf(v, "%d.%d", &maj, &min)
	return maj, min
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
