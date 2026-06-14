package siteops

import (
	"fmt"
	"os"

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

// FinishFrankenPHPLink performs the post-registration steps for a site whose
// runtime is "frankenphp": ensure the image is pulled, write a per-site
// quadlet that runs the framework's entrypoint, generate an nginx proxy
// vhost, update container hosts, and reload nginx.
func FinishFrankenPHPLink(site config.Site) error {
	entrypoint, env := site.FrankenPHPQuadletSpec()

	_ = podman.WriteContainerHosts()

	// Build the derived image (dunglas base + lerd's standard extension set) so
	// the site has redis/gd/pdo/... instead of the bare base. The build pulls the
	// base itself; a failure leaves the site registered to retry on next start.
	if err := podman.BuildFrankenPHPImage(site.PHPVersion, false, os.Stdout); err != nil {
		fmt.Printf("[WARN] building FrankenPHP image: %v\n", err)
	}

	// WriteFrankenPHPQuadletDiff ensures the debug-tooling bind-mount sources
	// (user ini, xdebug, dump, devtools) exist before referencing them.
	unitName := podman.FrankenPHPContainerName(site.Name)
	changed, err := podman.WriteFrankenPHPQuadletDiff(site.Name, site.Path, site.PHPVersion, entrypoint, env)
	if err != nil {
		return fmt.Errorf("writing FrankenPHP quadlet: %w", err)
	}
	_ = podman.DaemonReloadFn()

	// Always Start (no-op if already running). If the quadlet content changed
	// (new PHP version, worker flip, new entrypoint) we also need to restart
	// so the running container picks up the change, otherwise the updated
	// image/exec sits unused until the next manual restart.
	if err := podman.StartUnit(unitName); err != nil {
		fmt.Printf("[WARN] starting FrankenPHP container: %v\n", err)
	}
	if changed {
		if err := podman.RestartUnit(unitName); err != nil {
			fmt.Printf("[WARN] restarting FrankenPHP container after quadlet change: %v\n", err)
		}
	}

	if site.Secured {
		if err := certs.SecureSite(site); err != nil {
			return fmt.Errorf("securing site: %w", err)
		}
	} else {
		if err := nginx.GenerateFrankenPHPVhost(site); err != nil {
			return fmt.Errorf("generating FrankenPHP vhost: %w", err)
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

// FinishCustomFPMLink performs post-registration steps for a PHP site whose
// runtime is "fpm-custom": build the per-site image from the project's
// Containerfile (FROM the lerd base, so it keeps php-fpm and the extensions),
// write a per-site FPM quadlet that reuses every lerd mount, start the
// container, and generate a normal fastcgi vhost pointing at it.
func FinishCustomFPMLink(site config.Site, containerCfg *config.ContainerConfig) error {
	_ = podman.WriteContainerHosts()

	fmt.Printf("Building custom FPM image for %s...\n", site.Name)
	if err := podman.BuildCustomImage(site.Name, site.Path, containerCfg); err != nil {
		return fmt.Errorf("building custom FPM image: %w", err)
	}
	podman.StoreContainerfileHash(site.Name, site.Path, containerCfg)

	if err := podman.WriteCustomFPMQuadlet(site.Name, site.PHPVersion); err != nil {
		return fmt.Errorf("writing custom FPM quadlet: %w", err)
	}
	// Start first so the freshly generated unit is loaded (RestartUnit on a
	// not-yet-active unit races the quadlet generator), then restart so a
	// rebuilt image is picked up instead of the container lingering on the old
	// build. StartUnit is a no-op when already running.
	unitName := podman.CustomFPMContainerName(site.Name)
	if err := podman.StartUnit(unitName); err != nil {
		fmt.Printf("[WARN] starting custom FPM container: %v\n", err)
	} else {
		_ = podman.RestartUnit(unitName)
	}

	if site.Secured {
		if err := certs.SecureSite(site); err != nil {
			return fmt.Errorf("securing site: %w", err)
		}
	} else if err := nginx.GenerateVhost(site, site.PHPVersion); err != nil {
		return fmt.Errorf("generating vhost: %w", err)
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

// FinishHostProxyLink performs the post-registration steps for a host-proxy
// site: no container is built or started. It refreshes the host.containers.internal
// mapping (so nginx can reach the host), generates the proxy vhost, and reloads
// nginx. The dev-server process is started separately by the caller (in the cli
// package) because siteops must not import cli.
func FinishHostProxyLink(site config.Site) error {
	_ = podman.WriteContainerHosts()

	if site.Secured {
		if err := certs.SecureSite(site); err != nil {
			return fmt.Errorf("securing site: %w", err)
		}
	} else {
		if err := nginx.GenerateHostProxyVhost(site); err != nil {
			return fmt.Errorf("generating host-proxy vhost: %w", err)
		}
	}

	if err := nginx.Reload(); err != nil {
		return fmt.Errorf("nginx reload: %w", err)
	}

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
