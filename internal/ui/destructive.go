package ui

import (
	"regexp"
	"strings"
)

// destructiveCommandPatterns matches command strings that drop/wipe data.
// Used by both the list endpoint (filters them out of the UI) and the run
// endpoint (defense-in-depth: refuses to execute even if the user crafts a
// .lerd.yaml that bypasses the list filter).
//
// Each pattern is a regexp compiled against the FULL command shell string
// (the `command:` field, not just the canonical name). Substring match is
// enough: `php artisan migrate:fresh --seed --force` contains `migrate:fresh`.
//
// Patterns are intentionally conservative — if a command CAN drop data, it
// counts as destructive. False positives are preferable to false negatives
// because the user can always run the command via the CLI when they really
// want it (`lerd php artisan db:wipe`), with the audit trail that implies.
var destructiveCommandPatterns = []*regexp.Regexp{
	// Laravel / Eloquent destructive migration commands
	regexp.MustCompile(`(?i)\bmigrate:fresh\b`),
	regexp.MustCompile(`(?i)\bmigrate:reset\b`),
	regexp.MustCompile(`(?i)\bmigrate:rollback\b`),
	regexp.MustCompile(`(?i)\bmigrate:refresh\b`),
	regexp.MustCompile(`(?i)\bdb:wipe\b`),
	regexp.MustCompile(`(?i)\bdb:seed\s+--force.*--class=.*Truncate`),
	regexp.MustCompile(`(?i)\bschema:drop\b`),

	// Doctrine (Symfony) destructive
	regexp.MustCompile(`(?i)\bdoctrine:database:drop\b`),
	regexp.MustCompile(`(?i)\bdoctrine:schema:drop\b`),
	regexp.MustCompile(`(?i)\bdoctrine:fixtures:load\b`),
	regexp.MustCompile(`(?i)\bdoctrine:migrations:rollback\b`),

	// Queue / cache wipers (less critical but still data loss)
	regexp.MustCompile(`(?i)\bqueue:clear\b`),
	regexp.MustCompile(`(?i)\bqueue:flush\b`),
	regexp.MustCompile(`(?i)\bhorizon:purge\b`),
	regexp.MustCompile(`(?i)\bhorizon:forget\b`),

	// Tenant / multi-tenant wipers
	regexp.MustCompile(`(?i)\btenants:.*drop\b`),
	regexp.MustCompile(`(?i)\btenants:.*delete\b`),

	// Generic raw SQL/shell footguns
	regexp.MustCompile(`(?i)\bdrop\s+(table|database|schema)\b`),
	regexp.MustCompile(`(?i)\btruncate\s+table\b`),
	regexp.MustCompile(`(?i)\brm\s+-rf\s+/`),
}

// IsDestructiveCommand reports whether the given command shell string matches
// any of the deny-list patterns. Use this for both UI filtering and exec-time
// rejection. Returns true if `cmd` contains a destructive verb.
func IsDestructiveCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return false
	}
	for _, re := range destructiveCommandPatterns {
		if re.MatchString(trimmed) {
			return true
		}
	}
	return false
}
