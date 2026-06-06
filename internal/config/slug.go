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

// WorktreeUnitSlug sanitizes a worktree directory basename for use inside a
// systemd unit name, which treats a dot as the start of the unit-type suffix.
// Only dots are rewritten, so dot-free worktree dirs keep their existing unit
// names (no migration needed) while domain-named dirs (api.gonitro.com-feat)
// stop producing invalid unit names.
func WorktreeUnitSlug(wtBase string) string {
	return strings.ReplaceAll(wtBase, ".", "-")
}
