package php

import (
	"path/filepath"
	"regexp"
	"sort"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
)

var fpmQuadletRe = regexp.MustCompile(`^lerd-php(\d)(\d+)-fpm\.container$`)
var fpmContainerRe = regexp.MustCompile(`^lerd-php(\d)(\d+)-fpm$`)

// ListInstalled returns all PHP versions that have an FPM quadlet file or a
// running/existing Podman container, e.g. ["8.3", "8.4"]. The two sources are
// merged so users whose quadlet file is missing but whose container still
// exists are not excluded.
func ListInstalled() ([]string, error) {
	seen := map[string]bool{}

	// Source 1: quadlet files
	pattern := filepath.Join(config.QuadletDir(), "lerd-php*-fpm.container")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	for _, m := range matches {
		sub := fpmQuadletRe.FindStringSubmatch(filepath.Base(m))
		if len(sub) == 3 {
			seen[sub[1]+"."+sub[2]] = true
		}
	}

	// Source 1b: launchd plists (macOS — QuadletDir is always empty there)
	for _, v := range listInstalledFromServiceDir() {
		seen[v] = true
	}

	// Source 2: podman containers (catches installs where the quadlet is missing).
	// Read-only: this MUST stay free of side effects. Production heal of a
	// missing quadlet happens explicitly in install/start paths via
	// ensureFPMQuadlet. Calling restoreQuadlet here would daemon-reload the
	// user's real systemd from any test that triggered ListInstalled, and
	// trigger a worker-stopping cascade via BindsTo.
	for name := range podman.Cache.Snapshot() {
		sub := fpmContainerRe.FindStringSubmatch(name)
		if len(sub) != 3 {
			continue
		}
		seen[sub[1]+"."+sub[2]] = true
	}

	versions := make([]string, 0, len(seen))
	for v := range seen {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	return versions, nil
}

func quadletExists(version string) bool {
	short := version[0:1] + version[2:]
	return services.Mgr.ContainerUnitInstalled("lerd-php" + short + "-fpm")
}

// IsInstalled returns true if the given PHP version has an FPM quadlet.
func IsInstalled(version string) bool {
	versions, err := ListInstalled()
	if err != nil {
		return false
	}
	for _, v := range versions {
		if v == version {
			return true
		}
	}
	return false
}
