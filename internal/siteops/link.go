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
	PHP            string // best installed PHP version (clamped to framework range)
	Node           string // detected Node version
	PHPMin         string // framework minimum PHP version (empty if no framework)
	PHPMax         string // framework maximum PHP version (empty if no framework)
	SuggestedPHP   string // better PHP version to install (empty if current is optimal)
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

	_ = podman.EnsureXdebugIni(phpVersion)
	if err := podman.WriteFPMQuadlet(phpVersion); err == nil {
		_ = podman.DaemonReloadFn()
	}

	_ = podman.RewriteFPMQuadlets()
	_ = podman.WriteContainerHosts()

	if err := nginx.Reload(); err != nil {
		return fmt.Errorf("nginx reload: %w", err)
	}

	// Linking a site doesn't start a systemd unit, so the shared
	// AfterUnitChange hook wouldn't otherwise fire. Notify the hook
	// explicitly so the CLI/MCP processes ping lerd-ui (and lerd-ui's
	// own in-process handler invalidates the snapshot cache) and the
	// new site appears in every open dashboard tab.
	if podman.AfterUnitChange != nil {
		podman.AfterUnitChange("site:" + site.Name)
	}

	return nil
}

// FinishCustomLink performs the post-registration steps for a custom container
// site: build the image, write a dedicated quadlet, generate a proxy vhost,
// update container hosts, and reload nginx.
func FinishCustomLink(site config.Site, containerCfg *config.ContainerConfig) error {
	if podman.CustomImageUpToDate(site.Name, site.Path, containerCfg) {
		fmt.Println("Container image up to date, skipping build.")
	} else {
		if err := podman.BuildCustomImage(site.Name, site.Path, containerCfg); err != nil {
			return fmt.Errorf("building custom image: %w", err)
		}
		podman.StoreContainerfileHash(site.Name, site.Path, containerCfg)
	}

	// Pre-create the shared hosts file before writing the unit so that the
	// macOS WriteContainerUnit helper (which calls os.MkdirAll on every volume
	// source path) does not create a directory at the hosts-file path if the
	// file doesn't exist yet.  WriteFPMQuadlet does the same pre-creation step.
	_ = podman.WriteContainerHosts()

	if err := podman.WriteCustomContainerQuadlet(site.Name, site.Path, site.ContainerPort); err != nil {
		return fmt.Errorf("writing custom quadlet: %w", err)
	}
	_ = podman.DaemonReloadFn()

	// Start the custom container so the site is immediately reachable.
	if err := podman.StartUnit(podman.CustomContainerName(site.Name)); err != nil {
		fmt.Printf("[WARN] starting custom container: %v\n", err)
	}

	if site.Secured {
		if err := certs.SecureSite(site); err != nil {
			return fmt.Errorf("securing site: %w", err)
		}
	} else {
		if err := nginx.GenerateCustomVhost(site); err != nil {
			return fmt.Errorf("generating custom vhost: %w", err)
		}
	}

	_ = podman.WriteContainerHosts()

	if err := nginx.Reload(); err != nil {
		return fmt.Errorf("nginx reload: %w", err)
	}

	if podman.AfterUnitChange != nil {
		podman.AfterUnitChange("site:" + site.Name)
	}

	return nil
}
