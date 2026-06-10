package podman

import (
	"fmt"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// CustomFPMContainerName returns the per-site PHP-FPM container name for a site
// that serves PHP via fastcgi from its own custom-built image (a PHP project
// with a Containerfile and no port), e.g. "lerd-cfpm-myapp".
func CustomFPMContainerName(siteName string) string {
	return "lerd-cfpm-" + siteName
}

// FPMContainerName resolves the FPM container nginx fastcgi's to and the php
// shims exec into: a per-site container for custom-FPM sites, otherwise the
// shared lerd-php<version>-fpm container.
func FPMContainerName(site config.Site, version string) string {
	if site.IsCustomFPM() {
		return CustomFPMContainerName(site.Name)
	}
	return "lerd-php" + strings.ReplaceAll(version, ".", "") + "-fpm"
}

// WriteCustomFPMQuadlet writes a per-site PHP-FPM quadlet running the site's
// custom-built image (CustomImageName) under a per-site container name. It
// reuses the shared FPM template so the container inherits every lerd mount
// (xdebug, dumps, devtools, the bun volume, the shell), overriding only the
// Image and ContainerName. Ensures the shared per-version ini/assets exist
// first, like WriteFPMQuadlet.
func WriteCustomFPMQuadlet(siteName, version string) error {
	if err := EnsureUserIni(version); err != nil {
		return fmt.Errorf("creating user ini: %w", err)
	}
	if err := EnsureXdebugIni(version); err != nil {
		return fmt.Errorf("creating xdebug ini: %w", err)
	}
	if err := EnsureDumpAssets(); err != nil {
		return fmt.Errorf("ensuring dump assets: %w", err)
	}
	if err := EnsureProfilerAssets(); err != nil {
		return fmt.Errorf("ensuring profiler assets: %w", err)
	}
	if err := EnsureDevtoolsAssets(); err != nil {
		return fmt.Errorf("ensuring devtools assets: %w", err)
	}
	if err := ensureFPMHostsFile(); err != nil {
		return err
	}

	content, err := generateCustomFPMQuadlet(siteName, version)
	if err != nil {
		return err
	}
	if _, err := WriteQuadletDiff(CustomFPMContainerName(siteName), content); err != nil {
		return err
	}
	return DaemonReloadFn()
}

// generateCustomFPMQuadlet renders the per-site FPM quadlet content: the shared
// FPM template with Image and ContainerName overridden for the site's custom
// image. Pure (no IO), mirroring GenerateFrankenPHPQuadlet.
func generateCustomFPMQuadlet(siteName, version string) (string, error) {
	content, err := renderFPMQuadletContent(version)
	if err != nil {
		return "", err
	}
	short := strings.ReplaceAll(version, ".", "")
	content = strings.ReplaceAll(content, "Image=lerd-php"+short+"-fpm:local", "Image="+CustomImageName(siteName))
	content = strings.ReplaceAll(content, "ContainerName=lerd-php"+short+"-fpm", "ContainerName="+CustomFPMContainerName(siteName))
	content = strings.ReplaceAll(content, "Description=Lerd PHP "+version+" FPM", "Description=Lerd PHP "+version+" FPM (custom: "+siteName+")")
	return content, nil
}

// RemoveCustomFPMQuadlet removes the per-site custom FPM quadlet unit file.
func RemoveCustomFPMQuadlet(siteName string) error {
	return RemoveQuadlet(CustomFPMContainerName(siteName))
}
