package php

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"gopkg.in/yaml.v3"
)

// DetectExtensions reads composer.json in dir and returns the list of PHP extensions
// declared in the require map (ext-* keys), with the "ext-" prefix stripped.
// Returns an empty slice on any error (non-fatal).
func DetectExtensions(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return nil
	}
	var composer struct {
		Require map[string]string `json:"require"`
	}
	if err := json.Unmarshal(data, &composer); err != nil {
		return nil
	}
	var exts []string
	for key := range composer.Require {
		if strings.HasPrefix(key, "ext-") {
			exts = append(exts, strings.TrimPrefix(key, "ext-"))
		}
	}
	return exts
}

// DetectVersion detects the PHP version for the given directory.
// It checks, in order:
//  1. .lerd.yaml php_version field (explicit lerd override)
//  2. .php-version file (explicit per-project pin)
//  3. composer.json require.php semver (project requirement)
//  4. global config default
func DetectVersion(dir string) (string, error) {
	// 1. .lerd.yaml — explicit lerd override takes top priority
	lerdYaml := filepath.Join(dir, ".lerd.yaml")
	if data, err := os.ReadFile(lerdYaml); err == nil {
		var lerdCfg struct {
			PHPVersion string `yaml:"php_version"`
		}
		if yaml.Unmarshal(data, &lerdCfg) == nil && lerdCfg.PHPVersion != "" {
			return lerdCfg.PHPVersion, nil
		}
	}

	// 2. .php-version file — explicit per-project pin
	phpVersionFile := filepath.Join(dir, ".php-version")
	if data, err := os.ReadFile(phpVersionFile); err == nil {
		v := strings.TrimSpace(string(data))
		if v != "" {
			return v, nil
		}
	}

	// 3. composer.json require.php — pick the best installed version that
	//    satisfies the constraint (e.g. ^8.3 with 8.4 installed → 8.4).
	//    Falls back to the literal minimum from the constraint when no
	//    installed version matches.
	composerFile := filepath.Join(dir, "composer.json")
	if data, err := os.ReadFile(composerFile); err == nil {
		var composer struct {
			Require map[string]string `json:"require"`
		}
		if json.Unmarshal(data, &composer) == nil {
			if phpConstraint, ok := composer.Require["php"]; ok {
				if v := bestInstalledVersion(phpConstraint); v != "" {
					return v, nil
				}
				if v := parseComposerPHP(phpConstraint); v != "" {
					return v, nil
				}
			}
		}
	}

	// 4. global config default
	cfg, err := config.LoadGlobal()
	if err != nil {
		return "8.4", nil
	}
	return cfg.PHP.DefaultVersion, nil
}

// DetectVersionClamped detects the PHP version and clamps it to the given range.
// If phpMin/phpMax are empty, no clamping is applied.
func DetectVersionClamped(dir, phpMin, phpMax, fallback string) string {
	v, err := DetectVersion(dir)
	if err != nil {
		v = fallback
	}
	if phpMin != "" || phpMax != "" {
		v = ClampToRange(v, phpMin, phpMax)
	}
	return v
}

// ClampToRange checks if version falls within [min, max] and returns the best
// installed version within range if it doesn't. Returns the original version if
// min/max are empty or the version is already in range. This is used to respect
// framework PHP version constraints during site linking.
func ClampToRange(version, min, max string) string {
	if min == "" && max == "" {
		return version
	}

	major, minor := parseMajorMinor(version)
	if major < 0 {
		return version
	}

	inRange := true
	if min != "" {
		mMajor, mMinor := parseMajorMinor(min)
		if mMajor >= 0 && (major < mMajor || (major == mMajor && minor < mMinor)) {
			inRange = false
		}
	}
	if max != "" {
		mMajor, mMinor := parseMajorMinor(max)
		if mMajor >= 0 && (major > mMajor || (major == mMajor && minor > mMinor)) {
			inRange = false
		}
	}

	if inRange {
		return version
	}

	// Build a composer-style constraint from min/max and find best installed.
	var parts []string
	if min != "" {
		parts = append(parts, ">="+min)
	}
	if max != "" {
		parts = append(parts, "<="+max)
	}
	constraint := strings.Join(parts, " ")
	if best := bestInstalledVersion(constraint); best != "" {
		return best
	}

	// No installed version in range; fall back to min if set.
	if min != "" {
		return min
	}
	return version
}

// parseComposerPHP extracts a simple major.minor version from a composer PHP constraint.
// e.g. "^8.2" → "8.2", ">=8.1" → "8.1", "~8.3.0" → "8.3"
func parseComposerPHP(constraint string) string {
	re := regexp.MustCompile(`(\d+\.\d+)`)
	matches := re.FindStringSubmatch(constraint)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// bestInstalledVersion returns the highest installed PHP version that satisfies
// the given composer constraint, or "" if none match.
func bestInstalledVersion(constraint string) string {
	installed, err := ListInstalled()
	if err != nil || len(installed) == 0 {
		return ""
	}
	// Iterate from highest to lowest.
	for i := len(installed) - 1; i >= 0; i-- {
		if satisfiesConstraint(installed[i], constraint) {
			return installed[i]
		}
	}
	return ""
}

// satisfiesConstraint checks if a major.minor version satisfies a composer PHP
// constraint. Supports ^, ~, >=, >, <=, <, !=, exact, .*, and || (OR).
func satisfiesConstraint(version, constraint string) bool {
	major, minor := parseMajorMinor(version)
	if major < 0 {
		return false
	}

	// Handle OR constraints: "^8.1 || ^7.4"
	for _, alt := range strings.Split(constraint, "||") {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		if satisfiesSingle(major, minor, alt) {
			return true
		}
	}
	return false
}

func satisfiesSingle(major, minor int, constraint string) bool {
	// Handle AND constraints: ">=8.1 <8.5", ">=8.1,<8.5"
	parts := splitAND(constraint)
	if len(parts) > 1 {
		for _, p := range parts {
			if !satisfiesSingle(major, minor, p) {
				return false
			}
		}
		return true
	}

	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return true
	}

	// Wildcard: "8.*"
	if strings.HasSuffix(constraint, ".*") {
		wMajor, _ := parseMajorMinor(strings.TrimSuffix(constraint, ".*") + ".0")
		return major == wMajor
	}

	// Operators
	switch {
	case strings.HasPrefix(constraint, "^"):
		cMajor, cMinor := parseMajorMinor(stripOp(constraint, "^"))
		if cMajor < 0 {
			return false
		}
		return major == cMajor && minor >= cMinor
	case strings.HasPrefix(constraint, "~"):
		cMajor, cMinor := parseMajorMinor(stripOp(constraint, "~"))
		if cMajor < 0 {
			return false
		}
		// ~8.3 means >=8.3, <9.0; ~8.3.0 means >=8.3.0, <8.4.0
		if strings.Count(stripOp(constraint, "~"), ".") >= 2 {
			return major == cMajor && minor == cMinor
		}
		return major == cMajor && minor >= cMinor
	case strings.HasPrefix(constraint, ">="):
		cMajor, cMinor := parseMajorMinor(stripOp(constraint, ">="))
		return major > cMajor || (major == cMajor && minor >= cMinor)
	case strings.HasPrefix(constraint, ">"):
		cMajor, cMinor := parseMajorMinor(stripOp(constraint, ">"))
		return major > cMajor || (major == cMajor && minor > cMinor)
	case strings.HasPrefix(constraint, "<="):
		cMajor, cMinor := parseMajorMinor(stripOp(constraint, "<="))
		return major < cMajor || (major == cMajor && minor <= cMinor)
	case strings.HasPrefix(constraint, "<"):
		cMajor, cMinor := parseMajorMinor(stripOp(constraint, "<"))
		return major < cMajor || (major == cMajor && minor < cMinor)
	case strings.HasPrefix(constraint, "!="):
		cMajor, cMinor := parseMajorMinor(stripOp(constraint, "!="))
		return major != cMajor || minor != cMinor
	default:
		// Exact match: "8.3" or "8.3.0"
		cMajor, cMinor := parseMajorMinor(constraint)
		return major == cMajor && minor == cMinor
	}
}

// splitAND splits a constraint on space or comma boundaries, treating each
// part as an AND condition. E.g. ">=8.1 <8.5" → [">=8.1", "<8.5"].
func splitAND(s string) []string {
	// Replace commas with spaces, then split on whitespace.
	s = strings.ReplaceAll(s, ",", " ")
	var parts []string
	for _, p := range strings.Fields(s) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func stripOp(s, op string) string {
	return strings.TrimSpace(strings.TrimPrefix(s, op))
}

func parseMajorMinor(s string) (int, int) {
	s = strings.TrimSpace(s)
	// Strip trailing .patch if present (e.g. "8.3.0" → "8.3")
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 2 {
		return -1, -1
	}
	var major, minor int
	if _, err := fmt.Sscanf(parts[0]+"."+parts[1], "%d.%d", &major, &minor); err != nil {
		return -1, -1
	}
	return major, minor
}
