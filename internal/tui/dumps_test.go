package tui

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/dumps"
)

func TestAppendDump_DedupesByID(t *testing.T) {
	m := NewModel("test")
	m.appendDump(DumpEntry{ID: "a", Text: "first"})
	m.appendDump(DumpEntry{ID: "a", Text: "second"})
	if len(m.dumps) != 1 {
		t.Errorf("expected 1 entry after dedup, got %d", len(m.dumps))
	}
	if m.dumps[0].Text != "first" {
		t.Errorf("dedup kept the wrong copy: %q", m.dumps[0].Text)
	}
}

func TestAppendDump_CapsAtBufferLimit(t *testing.T) {
	m := NewModel("test")
	for i := 0; i < dumpsBufferCap+50; i++ {
		m.appendDump(DumpEntry{ID: rune2id(i)})
	}
	if len(m.dumps) != dumpsBufferCap {
		t.Errorf("len = %d, want %d", len(m.dumps), dumpsBufferCap)
	}
	// Oldest should be 50 (we sent 0..cap+49; first 50 evicted).
	if got := m.dumps[0].ID; got != rune2id(50) {
		t.Errorf("oldest = %q, want %q", got, rune2id(50))
	}
}

func TestToDumpEntry_CopiesNestedFields(t *testing.T) {
	ev := dumps.Event{
		ID: "x",
		TS: "2026-05-10T00:00:00.000Z",
		Ctx: dumps.Context{
			Type:    "fpm",
			Site:    "acme",
			Request: "GET /",
		},
		Src:   dumps.Source{File: "/x.php", Line: 12},
		Label: "user",
		Text:  "App\\Models\\User {#1}",
	}
	got := toDumpEntry(ev)
	if got.ID != "x" || got.Site != "acme" || got.Line != 12 || got.Label != "user" {
		t.Errorf("toDumpEntry drift: %+v", got)
	}
}

func TestDumpsContentLines_EmptyShowsHint(t *testing.T) {
	m := NewModel("test")
	lines, _ := dumpsContentLines(m, false, 80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "no dumps yet") {
		t.Errorf("empty state hint missing:\n%s", joined)
	}
	if !strings.Contains(joined, "lerd dump on") {
		t.Errorf("empty state should mention how to enable:\n%s", joined)
	}
}

func TestDumpsContentLines_ShowsHeaderAndPreview(t *testing.T) {
	m := NewModel("test")
	m.appendDump(DumpEntry{
		ID:      "a",
		TS:      "2026-05-10T12:34:56.000Z",
		Type:    "fpm",
		Site:    "acme",
		Request: "GET /users/1",
		File:    "/home/u/Code/acme/app/Foo.php",
		Line:    42,
		Label:   "user",
		Text:    "App\\Models\\User {#1\n  name: \"alice\"\n}",
	})
	lines, _ := dumpsContentLines(m, true, 100)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "fpm") {
		t.Errorf("ctx type missing: %q", joined)
	}
	if !strings.Contains(joined, "acme") {
		t.Errorf("site missing: %q", joined)
	}
	if !strings.Contains(joined, "/users/1") {
		t.Errorf("request missing: %q", joined)
	}
	if !strings.Contains(joined, "alice") {
		t.Errorf("preview text missing: %q", joined)
	}
}

func TestDumpPreviewLines_TruncatesLongOutput(t *testing.T) {
	e := DumpEntry{Text: "a\nb\nc\nd\ne\nf\ng"}
	got := dumpPreviewLines(e, 20)
	if len(got) > 5 {
		t.Errorf("expected at most 5 preview lines, got %d", len(got))
	}
	if !strings.Contains(strings.Join(got, "\n"), "more lines") {
		t.Errorf("expected truncation marker, got %v", got)
	}
}

func TestShortPath_UnchangedForShallow(t *testing.T) {
	if got := shortPath("/a/b/c"); got != "/a/b/c" {
		t.Errorf("shortPath drift = %q", got)
	}
	if got := shortPath("/home/u/Code/acme/app/Models/User.php"); !strings.HasPrefix(got, "...") {
		t.Errorf("shortPath should ellipsise long: %q", got)
	}
}

func rune2id(i int) string {
	// Pad with leading zeros so lex order matches insertion order.
	return string(rune('a')) + string(rune('0'+(i/100))) + string(rune('0'+((i/10)%10))) + string(rune('0'+(i%10)))
}
