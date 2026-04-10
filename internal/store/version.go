package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// composerLock is a minimal representation of composer.lock for version extraction.
type composerLock struct {
	Packages    []composerPackage `json:"packages"`
	PackagesDev []composerPackage `json:"packages-dev"`
}

type composerPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// DetectFrameworkVersion reads composer.lock in dir and returns the major version
// of the given composer package. Returns "" if the file is missing or the package
// is not found.
func DetectFrameworkVersion(dir string, composerPackageName string) string {
	data, err := os.ReadFile(filepath.Join(dir, "composer.lock"))
	if err != nil {
		return ""
	}

	var lock composerLock
	if json.Unmarshal(data, &lock) != nil {
		return ""
	}

	for _, pkg := range append(lock.Packages, lock.PackagesDev...) {
		if pkg.Name == composerPackageName {
			return extractMajorVersion(pkg.Version)
		}
	}
	return ""
}

// extractMajorVersion extracts the major version from a composer version string.
// Examples: "v11.34.2" -> "11", "7.0.0" -> "7", "v2.3.0-beta.1" -> "2"
func extractMajorVersion(version string) string {
	v := strings.TrimPrefix(version, "v")
	// Strip pre-release suffix
	if i := strings.IndexByte(v, '-'); i != -1 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 2)
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	return parts[0]
}
