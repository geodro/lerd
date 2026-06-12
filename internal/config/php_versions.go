package config

import (
	"strconv"
	"strings"
)

// SupportedPHPVersions lists the PHP versions lerd can build FPM images for.
// 7.4 and 8.0 are a frozen legacy tier for old projects: still buildable from
// Alpine 3.16, but pinned (older xdebug, no mongodb ext) and not security-updated.
var SupportedPHPVersions = []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"}

// FrankenPHPMinVersion is the oldest PHP version dunglas/frankenphp publishes an
// image for; supported versions below it run under FPM only.
const FrankenPHPMinVersion = "8.2"

// IsSupportedPHPVersion reports whether v is a version lerd can install.
func IsSupportedPHPVersion(v string) bool {
	for _, s := range SupportedPHPVersions {
		if s == v {
			return true
		}
	}
	return false
}

// FrankenPHPVersions returns the subset of SupportedPHPVersions that
// dunglas/frankenphp publishes an image for, in the same ascending order.
func FrankenPHPVersions() []string {
	var out []string
	for _, v := range SupportedPHPVersions {
		if phpVersionAtLeast(v, FrankenPHPMinVersion) {
			out = append(out, v)
		}
	}
	return out
}

// IsFrankenPHPVersion reports whether dunglas/frankenphp publishes an image for v.
func IsFrankenPHPVersion(v string) bool {
	return IsSupportedPHPVersion(v) && phpVersionAtLeast(v, FrankenPHPMinVersion)
}

// LatestFrankenPHPVersion returns the newest PHP version dunglas/frankenphp
// publishes an image for, used as the fallback for unsupported versions. Falls
// back to FrankenPHPMinVersion so callers never index an empty slice.
func LatestFrankenPHPVersion() string {
	v := FrankenPHPVersions()
	if len(v) == 0 {
		return FrankenPHPMinVersion
	}
	return v[len(v)-1]
}

// phpVersionAtLeast reports whether "major.minor" version a is >= b.
func phpVersionAtLeast(a, b string) bool {
	amaj, amin := splitMajorMinor(a)
	bmaj, bmin := splitMajorMinor(b)
	if amaj != bmaj {
		return amaj > bmaj
	}
	return amin >= bmin
}

func splitMajorMinor(v string) (int, int) {
	parts := strings.SplitN(v, ".", 2)
	major, _ := strconv.Atoi(parts[0])
	minor := 0
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	return major, minor
}
