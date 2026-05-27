package tui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// lowerRune folds r to its lowercase form. Wrapped so toLowerRune in the
// highlight path stays self-contained.
func lowerRune(r rune) rune { return unicode.ToLower(r) }

// logErrorKeywords / logWarnKeywords are matched case-insensitively against
// each tailed line so users can spot framework errors or worker warnings
// without re-reading whole paragraphs. Kept simple substring-style because
// the goal is to glance, not to parse — every Laravel / nginx / fpm /
// systemd convention contains one of these in its severity prefix.
var (
	logErrorKeywords = []string{"error", "fatal", "panic", "exception", "critical"}
	logWarnKeywords  = []string{"warn", "warning", "deprecated"}
)

// logHighlightStyle picks the foreground colour for a fully-coloured log
// line. Error lines turn red, warnings amber, everything else unstyled.
var (
	logErrorStyle  = lipgloss.NewStyle().Foreground(colFailing)
	logWarnStyle   = lipgloss.NewStyle().Foreground(colPaused)
	logMatchStyle  = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("#000000"))
	logDimNonMatch = lipgloss.NewStyle().Foreground(colDim)
)

// styleLogLine adds severity colouring and (when a filter is active) match
// highlighting. Non-matching lines render dimmed so the matches pop. Empty
// filter just paints the severity colour.
func styleLogLine(line, filter string) string {
	severity := classifyLogLine(line)
	needle := strings.TrimSpace(filter)
	if needle == "" {
		return paintBySeverity(line, severity)
	}
	if !strings.Contains(strings.ToLower(line), strings.ToLower(needle)) {
		return logDimNonMatch.Render(line)
	}
	return highlightMatches(paintBySeverity(line, severity), line, needle)
}

// classifyLogLine returns "error", "warn", or "" based on a case-insensitive
// keyword scan. Cheap enough to run per line — the substring search is O(n)
// over a handful of short keywords.
func classifyLogLine(line string) string {
	low := strings.ToLower(line)
	for _, k := range logErrorKeywords {
		if strings.Contains(low, k) {
			return "error"
		}
	}
	for _, k := range logWarnKeywords {
		if strings.Contains(low, k) {
			return "warn"
		}
	}
	return ""
}

func paintBySeverity(line, severity string) string {
	switch severity {
	case "error":
		return logErrorStyle.Render(line)
	case "warn":
		return logWarnStyle.Render(line)
	}
	return line
}

// highlightMatches wraps every occurrence of needle in source with a match
// background. Rune-by-rune walk so Unicode case folding (e.g. Turkish İ
// folding to 'i', changing byte length) doesn't misalign offsets between
// the lowered and original strings. Case-insensitive: uses unicode.ToLower
// on each rune when comparing against the lowered needle.
func highlightMatches(painted, raw, needle string) string {
	// Severity styling around the whole line wraps with a leading SGR
	// code that the splice would corrupt; fall back to the raw text when
	// the two differ and let the match style stand alone.
	source := painted
	if painted != raw {
		source = raw
	}
	needleRunes := []rune(strings.ToLower(needle))
	if len(needleRunes) == 0 {
		return source
	}
	srcRunes := []rune(source)
	lowSrc := make([]rune, len(srcRunes))
	for i, r := range srcRunes {
		lowSrc[i] = toLowerRune(r)
	}

	var b strings.Builder
	i := 0
	for i < len(srcRunes) {
		idx := runeIndex(lowSrc[i:], needleRunes)
		if idx < 0 {
			b.WriteString(string(srcRunes[i:]))
			break
		}
		b.WriteString(string(srcRunes[i : i+idx]))
		b.WriteString(logMatchStyle.Render(string(srcRunes[i+idx : i+idx+len(needleRunes)])))
		i += idx + len(needleRunes)
	}
	return b.String()
}

// runeIndex returns the rune-offset of the first occurrence of needle in
// haystack, or -1. Mirrors strings.Index but on []rune so the result is
// safe to use as a slice index into srcRunes.
func runeIndex(haystack, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j, r := range needle {
			if haystack[i+j] != r {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// toLowerRune is a thin wrapper to keep the highlight helper readable;
// using strings.ToLower(string(r))[0] would re-allocate per rune and
// silently fall back to the byte form, which is what we're trying to
// avoid. Drops down to the stdlib unicode helper for the actual fold.
func toLowerRune(r rune) rune {
	return lowerRune(r)
}
