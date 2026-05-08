package config

import "strings"

// SiteSlug converts a name (site name, branch, directory basename, or domain)
// to a database-safe, underscore-separated slug. Hyphens and dots are replaced
// with underscores and the result is lowercased.
func SiteSlug(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	return s
}
