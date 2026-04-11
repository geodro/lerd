package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DetectFrameworkVersion reads composer.json in dir and returns the major version
// of the given composer package. It searches require, require-dev, and any extra
// sections specified. If the constraint is unparseable (e.g. "*"), it falls back
// to the versionKey dot-path in composer.json.
func DetectFrameworkVersion(dir string, composerPackageName string, extraSections ...string) string {
	return DetectFrameworkVersionWithKey(dir, composerPackageName, "", extraSections...)
}

// DetectFrameworkVersionWithKey is like DetectFrameworkVersion but also accepts a
// versionKey (dot-path into composer.json) as a fallback when the constraint is "*".
func DetectFrameworkVersionWithKey(dir string, composerPackageName string, versionKey string, extraSections ...string) string {
	data, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if err != nil {
		return ""
	}

	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return ""
	}

	sections := append([]string{"require", "require-dev"}, extraSections...)
	for _, section := range sections {
		chunk, ok := raw[section]
		if !ok {
			continue
		}
		var m map[string]string
		if json.Unmarshal(chunk, &m) != nil {
			continue
		}
		constraint, found := m[composerPackageName]
		if !found {
			continue
		}
		if v := extractMajorFromConstraint(constraint); v != "" {
			return v
		}
		if versionKey != "" {
			if v := resolveJSONPath(raw, versionKey); v != "" {
				return extractMajorFromConstraint(v)
			}
		}
	}
	return ""
}

// resolveJSONPath walks a dot-separated path through nested JSON objects.
func resolveJSONPath(raw map[string]json.RawMessage, path string) string {
	parts := strings.Split(path, ".")
	current := raw
	for i, part := range parts {
		chunk, ok := current[part]
		if !ok {
			return ""
		}
		if i == len(parts)-1 {
			var s string
			if json.Unmarshal(chunk, &s) == nil {
				return s
			}
			return ""
		}
		var next map[string]json.RawMessage
		if json.Unmarshal(chunk, &next) != nil {
			return ""
		}
		current = next
	}
	return ""
}

// extractMajorFromConstraint extracts the major version from a composer constraint.
// Examples: "^11.0" -> "11", "~7.1" -> "7", ">=10.0 <11.0" -> "10", "^5.0|^6.0" -> "5"
func extractMajorFromConstraint(constraint string) string {
	for i := 0; i < len(constraint); i++ {
		b := constraint[i]
		if b >= '0' && b <= '9' {
			j := i
			for j < len(constraint) && constraint[j] >= '0' && constraint[j] <= '9' {
				j++
			}
			return constraint[i:j]
		}
	}
	return ""
}

// DetectVersionFromFile reads a file and extracts the major version using a regex pattern.
func DetectVersionFromFile(dir, relPath, pattern string) string {
	data, err := os.ReadFile(filepath.Join(dir, relPath))
	if err != nil {
		return ""
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	m := re.FindSubmatch(data)
	if len(m) < 2 {
		return ""
	}
	return extractMajorFromConstraint(string(m[1]))
}

// extractMajorVersion extracts the major version from a composer version string.
// Examples: "v11.34.2" -> "11", "7.0.0" -> "7", "v2.3.0-beta.1" -> "2"
func extractMajorVersion(version string) string {
	v := strings.TrimPrefix(version, "v")
	if i := strings.IndexByte(v, '-'); i != -1 {
		v = v[:i]
	}
	parts := strings.SplitN(v, ".", 2)
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	return parts[0]
}
