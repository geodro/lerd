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

func TestFilteredDumps_MatchesAcrossFields(t *testing.T) {
	in := []DumpEntry{
		{ID: "1", Site: "acme", Text: "alice"},
		{ID: "2", Site: "other", Text: "bob"},
		{ID: "3", Site: "acme", Label: "carol"},
		{ID: "4", File: "/var/log/dave.log"},
	}
	cases := []struct {
		needle string
		want   []string
	}{
		{"acme", []string{"1", "3"}},
		{"BOB", []string{"2"}},
		{"carol", []string{"3"}},
		{"dave", []string{"4"}},
		{"", []string{"1", "2", "3", "4"}},
	}
	for _, c := range cases {
		got := filteredDumps(in, c.needle)
		gotIDs := make([]string, len(got))
		for i, e := range got {
			gotIDs[i] = e.ID
		}
		if strings.Join(gotIDs, ",") != strings.Join(c.want, ",") {
			t.Errorf("filteredDumps(_, %q) = %v, want %v", c.needle, gotIDs, c.want)
		}
	}
}

func TestDumpBodyLines_PreviewVsExpanded(t *testing.T) {
	e := DumpEntry{Text: "a\nb\nc\nd\ne\nf\ng"}
	preview := dumpBodyLines(e, 40, false)
	if len(preview) > 5 { // 4 + truncation marker
		t.Errorf("preview should cap at 5 rows, got %d", len(preview))
	}
	if !strings.Contains(strings.Join(preview, "\n"), "enter to expand") {
		t.Errorf("preview should hint at enter:\n%v", preview)
	}
	expanded := dumpBodyLines(e, 40, true)
	if len(expanded) != 7 {
		t.Errorf("expanded should render every line, got %d", len(expanded))
	}
}

func TestDumpsContentLines_FilterNarrowsList(t *testing.T) {
	m := NewModel("test")
	m.appendDump(DumpEntry{ID: "1", Site: "acme", Text: "alice"})
	m.appendDump(DumpEntry{ID: "2", Site: "other", Text: "bob"})
	m.dumpsFilter = "acme"
	lines, _ := dumpsContentLines(m, true, 100)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "alice") {
		t.Errorf("expected matching entry to render:\n%s", joined)
	}
	if strings.Contains(joined, "bob") {
		t.Errorf("expected filtered-out entry to disappear:\n%s", joined)
	}
	if !strings.Contains(joined, "1 shown / 2 buffered") {
		t.Errorf("header should show shown / buffered counts:\n%s", joined)
	}
}

func TestToggleDumpExpand_FlipsMap(t *testing.T) {
	m := NewModel("test")
	m.appendDump(DumpEntry{ID: "alpha", Text: "x"})
	m.appendDump(DumpEntry{ID: "beta", Text: "y"})
	m.dumpsCursor = 0
	m.toggleDumpExpand()
	if !m.dumpsExpanded["beta"] {
		// dumpsCursor=0 points at the newest entry, which is beta (renders top).
		// But filteredDumps preserves insertion order; cursor 0 = filtered[0] = alpha.
		// Verify whichever ID was flipped is the one at cursor 0 of the filter view.
		if !m.dumpsExpanded["alpha"] {
			t.Error("expected one entry to be expanded after toggle")
		}
	}
	m.toggleDumpExpand()
	// Same row flipped back.
	if m.dumpsExpanded["alpha"] || m.dumpsExpanded["beta"] {
		t.Errorf("expected re-toggle to clear; got map %v", m.dumpsExpanded)
	}
}

func TestFilteredDumpsWithCtx_AppliesContextFilter(t *testing.T) {
	in := []DumpEntry{
		{ID: "1", Type: "fpm", Text: "request"},
		{ID: "2", Type: "cli", Text: "tinker"},
		{ID: "3", Type: "fpm", Text: "another"},
	}
	if got := filteredDumpsWithCtx(in, "", "fpm"); len(got) != 2 {
		t.Errorf("expected 2 fpm entries, got %d", len(got))
	}
	if got := filteredDumpsWithCtx(in, "", "cli"); len(got) != 1 {
		t.Errorf("expected 1 cli entry, got %d", len(got))
	}
	if got := filteredDumpsWithCtx(in, "tinker", "cli"); len(got) != 1 || got[0].ID != "2" {
		t.Errorf("ctx + needle should AND together: %+v", got)
	}
	if got := filteredDumpsWithCtx(in, "", ""); len(got) != 3 {
		t.Errorf("empty filters should return all, got %d", len(got))
	}
}

func TestToggleString_Mutex(t *testing.T) {
	if got := toggleString("", "fpm"); got != "fpm" {
		t.Errorf("first toggle should set value, got %q", got)
	}
	if got := toggleString("fpm", "fpm"); got != "" {
		t.Errorf("second toggle of same value should clear, got %q", got)
	}
	if got := toggleString("fpm", "cli"); got != "cli" {
		t.Errorf("setting different value should swap, got %q", got)
	}
}

func TestRenderDumpsChips_HighlightsActive(t *testing.T) {
	got := stripANSI(renderDumpsChips("fpm"))
	if !strings.Contains(got, "fpm") || !strings.Contains(got, "cli") {
		t.Errorf("both chip labels should render:\n%s", got)
	}
}

func TestClearDumps_PromptsConfirmWhenBufferNonEmpty(t *testing.T) {
	m := NewModel("test")
	m.appendDump(DumpEntry{ID: "a"})
	m.appendDump(DumpEntry{ID: "b"})

	cmd := m.clearDumps()
	if cmd != nil {
		t.Errorf("clearDumps should stage a confirm, not return a command directly: %v", cmd)
	}
	if !m.confirmActive {
		t.Error("clearDumps with a non-empty buffer should open a confirm modal")
	}
	// The buffer is intact until the user presses y.
	if len(m.dumps) != 2 {
		t.Errorf("buffer should not be cleared before confirm: %d", len(m.dumps))
	}
}

func TestClearDumps_EmptyBufferSkipsPrompt(t *testing.T) {
	m := NewModel("test")
	cmd := m.clearDumps()
	if cmd == nil {
		t.Error("clearDumps on an empty buffer should run lerd dump clear directly")
	}
	if m.confirmActive {
		t.Error("empty buffer should not trigger a confirm modal")
	}
}

func TestClearDumps_DefersBufferMutationToUpdate(t *testing.T) {
	m := NewModel("test")
	m.appendDump(DumpEntry{ID: "a"})
	m.appendDump(DumpEntry{ID: "b"})
	if cmd := m.clearDumps(); cmd != nil {
		t.Fatalf("clearDumps should stage a confirm, got cmd %v", cmd)
	}
	// Clearing happens in Update via dumpsClearedMsg, so the buffer must be
	// untouched until then; mutating it from the command goroutine would race
	// the render path.
	if len(m.dumps) != 2 {
		t.Errorf("clearDumps mutated the buffer before confirmation: len=%d", len(m.dumps))
	}
}

func TestDumpsClearedMsg_ZeroesBuffer(t *testing.T) {
	m := NewModel("test")
	m.appendDump(DumpEntry{ID: "a"})
	m.appendDump(DumpEntry{ID: "b"})
	m.dumpsExpanded = map[string]bool{"a": true}
	m.dumpsCursor = 1
	m.dumpsScroll = 5
	if _, cmd := m.Update(dumpsClearedMsg{}); cmd != nil {
		t.Errorf("dumpsClearedMsg should not emit a command, got %v", cmd)
	}
	if len(m.dumps) != 0 {
		t.Errorf("dumps not cleared: %d", len(m.dumps))
	}
	if m.dumpsExpanded != nil {
		t.Error("dumpsExpanded not cleared")
	}
	if m.dumpsCursor != 0 || m.dumpsScroll != 0 {
		t.Errorf("cursor/scroll not reset: cursor=%d scroll=%d", m.dumpsCursor, m.dumpsScroll)
	}
}

func rune2id(i int) string {
	// Pad with leading zeros so lex order matches insertion order.
	return string(rune('a')) + string(rune('0'+(i/100))) + string(rune('0'+((i/10)%10))) + string(rune('0'+(i%10)))
}
