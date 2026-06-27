//go:build linux

package systemd

import (
	"strings"
	"testing"
)

func TestFormatUnitFailureDetail(t *testing.T) {
	cases := []struct {
		name   string
		header string
		logs   string
		want   string
	}{
		{"empty", "", "", ""},
		{"header only", "result: exit-code, exit status 125", "",
			" (result: exit-code, exit status 125)"},
		{"logs only", "", "    lerd-nginx: name is already in use",
			"\n    lerd-nginx: name is already in use"},
		{"both", "result: exit-code", "    boom",
			" (result: exit-code)\n    boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatUnitFailureDetail(tc.header, tc.logs); got != tc.want {
				t.Errorf("formatUnitFailureDetail(%q, %q) = %q, want %q",
					tc.header, tc.logs, got, tc.want)
			}
		})
	}
}

// journalTail must degrade to "" rather than panic or surface noise when the
// unit has no journal (or journalctl is missing entirely, e.g. a container
// without systemd) — the enrichment is best-effort and never the failure path.
func TestJournalTailNoEntries(t *testing.T) {
	got := journalTail("lerd-nonexistent-unit-xyz.service", 5)
	if got != "" {
		t.Errorf("journalTail for a unit with no journal = %q, want empty", got)
	}
}

// Every emitted journal line is indented so it reads as a nested detail block
// under the one-line error, never flush against the left margin.
func TestJournalTailIndents(t *testing.T) {
	if out := journalTail("init.scope", 3); out != "" {
		for _, ln := range strings.Split(out, "\n") {
			if !strings.HasPrefix(ln, "    ") {
				t.Errorf("journal line not indented: %q", ln)
			}
		}
	}
}
