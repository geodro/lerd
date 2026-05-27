package tui

import (
	"runtime"
	"strings"
	"testing"
)

// TestSystemRows_ContainsCoreSections checks every section header the system
// page promises (DNS, Nginx, Watcher, Notifications, Dump bridge, PHP, Node,
// Lerd) is rendered. Worker mode is platform-gated and tested separately.
func TestSystemRows_ContainsCoreSections(t *testing.T) {
	m := NewModel("test")
	rows := m.systemRows()

	want := []string{"DNS", "Nginx", "Watcher", "Notifications", "Dump bridge", "PHP versions", "Node", "Lerd"}
	have := map[string]bool{}
	for _, r := range rows {
		if r.kind == sysHeader {
			have[r.label] = true
		}
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing section header %q in system rows", w)
		}
	}
}

// TestSystemRows_WorkerModeOnlyOnDarwin matches the existing settings rule —
// Linux always runs workers under systemd, so a worker-mode toggle there is
// meaningless and must not be advertised.
func TestSystemRows_WorkerModeOnlyOnDarwin(t *testing.T) {
	m := NewModel("test")
	rows := m.systemRows()

	found := false
	for _, r := range rows {
		if r.kind == sysWorkerMode {
			found = true
			break
		}
	}

	wantPresent := runtime.GOOS == "darwin"
	if found != wantPresent {
		t.Errorf("worker-mode row present=%v on %s, want present=%v",
			found, runtime.GOOS, wantPresent)
	}
}

// TestNavigableSystemRows_SkipsHeadersAndInfo verifies the cursor only lands
// on interactive rows; header and info rows are scenery.
func TestNavigableSystemRows_SkipsHeadersAndInfo(t *testing.T) {
	rows := []systemRow{
		{kind: sysHeader, label: "X"},
		{kind: sysInfo, label: "a"},
		{kind: sysDumpsEnabled, label: "Dumps"},
		{kind: sysInfo, label: "b"},
		{kind: sysNotifEnabled, label: "Notif"},
		{kind: sysHeader, label: "Y"},
		{kind: sysAutostart, label: "Auto"},
	}
	nav := navigableSystemRows(rows)
	if len(nav) != 3 {
		t.Fatalf("expected 3 navigable rows, got %d", len(nav))
	}
	for _, idx := range nav {
		kind := rows[idx].kind
		if kind == sysHeader || kind == sysInfo {
			t.Errorf("navigable index %d points to non-interactive kind %v", idx, kind)
		}
	}
}

// TestSystemContentLines_RendersHeader checks the rendered output contains
// the System title and the return-key hint so users discover how to leave.
func TestSystemContentLines_RendersHeader(t *testing.T) {
	m := NewModel("test")
	lines, _ := systemContentLinesWithCursor(m, false, 100)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "System") {
		t.Errorf("system page should render title:\n%s", joined)
	}
	if !strings.Contains(joined, "Y or esc") {
		t.Errorf("system page should hint at the return key:\n%s", joined)
	}
}

// TestSystemContentLines_CursorLineLandsOnInteractiveRow ensures the
// reported cursor row index actually corresponds to a toggleable row in
// the output, so the viewport will keep the selection visible.
func TestSystemContentLines_CursorLineLandsOnInteractiveRow(t *testing.T) {
	m := NewModel("test")
	m.systemRow = 0
	lines, cursorLine := systemContentLinesWithCursor(m, true, 100)
	if cursorLine == 0 {
		// 0 means "no interactive row rendered" — the page must always have
		// at least the notifications toggle, so this is a regression.
		t.Fatal("expected cursorLine > 0 when at least one interactive row exists")
	}
	if cursorLine >= len(lines) {
		t.Fatalf("cursorLine %d out of bounds (%d lines)", cursorLine, len(lines))
	}
	// The selected line should carry the inverted accent prefix used by
	// renderDetailRow — "▸" is the universal marker for the focused row.
	if !strings.Contains(lines[cursorLine], "▸") {
		t.Errorf("cursor line %q lacks the ▸ marker", lines[cursorLine])
	}
}

// TestSystemRows_DumpsInfoShowsBufferedCount uses the actual buffered count
// from the model so users see the same number the Dumps view shows.
func TestSystemRows_DumpsInfoShowsBufferedCount(t *testing.T) {
	m := NewModel("test")
	m.appendDump(DumpEntry{ID: "a"})
	m.appendDump(DumpEntry{ID: "b"})

	rows := m.systemRows()
	var bufferedRow systemRow
	for _, r := range rows {
		if r.kind == sysInfo && r.label == "Buffered" {
			bufferedRow = r
			break
		}
	}
	if !strings.Contains(bufferedRow.value, "2 events") {
		t.Errorf("expected 'Buffered: 2 events', got %q", bufferedRow.value)
	}
}
