// Package wsl holds the WSL2-specific detection and config-patching logic that
// `lerd wsl:setup` and the doctor's WSL checks share. The file-patch helpers are
// pure (content in, content out) so they're unit-testable without touching real
// /etc/wsl.conf, ~/.config/containers/containers.conf, or %USERPROFILE%\.wslconfig.
package wsl

import (
	"os"
	"regexp"
	"strings"
)

// IsWSL reports whether we're running inside a WSL2 distro. WSL sets
// WSL_DISTRO_NAME in every shell, and the kernel release carries "microsoft"
// (WSL2) or "WSL"; either is conclusive.
func IsWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		s := strings.ToLower(string(b))
		if strings.Contains(s, "microsoft") || strings.Contains(s, "wsl") {
			return true
		}
	}
	return false
}

// EnsureSectionLine guarantees an ini/toml file's `[section]` contains a line
// assigning `key`, setting it to `line` (the full `key=value` text, so callers
// control quoting and spacing). It returns the updated content and whether
// anything changed: a key already set to `line` is left untouched (idempotent),
// a key set to something else is rewritten, a missing key is inserted under an
// existing section, and a missing section is appended. Good enough for the flat,
// hand-edited config files WSL setup touches; not a general TOML parser.
func EnsureSectionLine(content, section, key, line string) (string, bool) {
	keyRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	sectionHeader := "[" + section + "]"

	lines := strings.Split(content, "\n")
	inSection := false
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if trimmed == sectionHeader {
				inSection = true
			} else if inSection {
				// Left our section without finding the key — insert at its end.
				return insertAt(lines, i, line), true
			} else {
				inSection = false
			}
			continue
		}
		if inSection && keyRe.MatchString(l) {
			if strings.TrimSpace(l) == strings.TrimSpace(line) {
				return content, false // already set as desired
			}
			lines[i] = line
			return strings.Join(lines, "\n"), true
		}
	}

	if inSection {
		// Section was the last block and the key was absent — append the line.
		return strings.Join(append(lines, line), "\n"), true
	}

	// Section absent entirely — append it.
	prefix := content
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		prefix += "\n"
	}
	return prefix + sectionHeader + "\n" + line + "\n", true
}

// insertAt splices line into lines just before index i, returning the joined text.
func insertAt(lines []string, i int, line string) string {
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:i]...)
	out = append(out, line)
	out = append(out, lines[i:]...)
	return strings.Join(out, "\n")
}

// WSLConfigLines are the [wsl2] settings lerd recommends in %USERPROFILE%\.wslconfig:
// mirrored networking so Windows browsers can reach *.test / *.localhost, with the
// related DNS/firewall/proxy toggles. Deliberately omits localhostForwarding and
// pageReporting, which are a no-op under mirrored mode and an unrecognized key
// respectively, and so spam WSL with warnings on every terminal launch.
var WSLConfigLines = []struct{ Key, Line string }{
	{"networkingMode", "networkingMode=mirrored"},
	{"dnsTunneling", "dnsTunneling=true"},
	{"firewall", "firewall=true"},
	{"autoProxy", "autoProxy=true"},
}

// HasEventsLoggerJournald reports whether a containers.conf already sets
// events_logger to journald, used by the doctor to flag the misconfiguration
// that breaks every `podman logs --follow` (and thus every dashboard log pane)
// on a systemd WSL host.
func HasEventsLoggerJournald(content string) bool {
	re := regexp.MustCompile(`(?m)^\s*events_logger\s*=\s*"?journald"?`)
	return re.MatchString(content)
}
