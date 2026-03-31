// Package version holds build-time version information injected via ldflags.
package version

import "fmt"

// These variables are set at build time via:
//
//	-X github.com/geodro/lerd/internal/version.Version=<tag>
//	-X github.com/geodro/lerd/internal/version.Commit=<sha>
//	-X github.com/geodro/lerd/internal/version.Date=<iso8601>
var (
	Version = "1.2.4"
	Commit  = "none"
	Date    = "unknown"
)

// String returns the full version string shown by `lerd --version`.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
