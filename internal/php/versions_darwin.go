//go:build darwin

package php

import (
	"os"
	"path/filepath"
	"regexp"
)

var fpmPlistRe = regexp.MustCompile(`^lerd-php(\d)(\d+)-fpm\.plist$`)

// listInstalledFromServiceDir returns PHP versions found as launchd plists in
// ~/Library/LaunchAgents. On macOS, plists replace systemd quadlet files, so
// the QuadletDir glob in ListInstalled always returns nothing.
func listInstalledFromServiceDir() []string {
	home, _ := os.UserHomeDir()
	pattern := filepath.Join(home, "Library", "LaunchAgents", "lerd-php*-fpm.plist")
	matches, _ := filepath.Glob(pattern)
	var versions []string
	for _, m := range matches {
		sub := fpmPlistRe.FindStringSubmatch(filepath.Base(m))
		if len(sub) == 3 {
			versions = append(versions, sub[1]+"."+sub[2])
		}
	}
	return versions
}
