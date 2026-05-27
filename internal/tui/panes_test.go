package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/geodro/lerd/internal/siteinfo"
)

func TestPadToWidth_PadsShort(t *testing.T) {
	got := padToWidth("hi", 6)
	if ansi.StringWidth(got) != 6 {
		t.Fatalf("expected width 6, got %d (%q)", ansi.StringWidth(got), got)
	}
}

func TestPadToWidth_NoOpWhenLongEnough(t *testing.T) {
	got := padToWidth("already long", 4)
	if got != "already long" {
		t.Fatalf("expected unchanged, got %q", got)
	}
}

func TestClipLine_TruncatesByDisplayWidth(t *testing.T) {
	got := clipLine("hello world", 5)
	if w := ansi.StringWidth(got); w > 5 {
		t.Fatalf("clip exceeded width %d: %q", w, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated string should end with …: %q", got)
	}
}

func TestClipLine_NoOpWhenShort(t *testing.T) {
	got := clipLine("hi", 20)
	if got != "hi" {
		t.Fatalf("expected unchanged, got %q", got)
	}
}

func TestTruncatePlain_RuneAware(t *testing.T) {
	// ü and é are multi-byte UTF-8 runes; must not slice mid-byte.
	got := truncatePlain("résümé-extra", 6)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	if len([]rune(got)) != 6 {
		t.Fatalf("expected 6 runes, got %d (%q)", len([]rune(got)), got)
	}
}

func TestPadRight_UsesRuneCount(t *testing.T) {
	got := padRight("é", 3)
	if len([]rune(got)) != 3 {
		t.Fatalf("expected 3 runes, got %d (%q)", len([]rune(got)), got)
	}
}

func TestRenderSiteRow_AlignsPHPColumn(t *testing.T) {
	// Two sites with different worker load should produce rows where the
	// PHP column starts at the same visual column — the bug that prompted
	// siteWorkerColWidth. We can't easily measure visual column directly,
	// but we can assert that the two rows have the same display width.
	s1 := siteinfo.EnrichedSite{Name: "a", PHPVersion: "8.3", HasQueueWorker: true, QueueRunning: true}
	s2 := siteinfo.EnrichedSite{Name: "b", PHPVersion: "8.3"}
	r1 := renderSiteRow(false, s1, 60)
	r2 := renderSiteRow(false, s2, 60)
	if w1, w2 := ansi.StringWidth(r1), ansi.StringWidth(r2); w1 != w2 {
		t.Fatalf("rows of equal pane width should have equal display width, got %d vs %d", w1, w2)
	}
}

func TestRenderServiceRow_AlignsMetaColumn(t *testing.T) {
	s1 := ServiceRow{Name: "mysql", State: stateRunning, SiteCount: 2, Pinned: true}
	s2 := ServiceRow{Name: "redis", State: stateStopped, SiteCount: 1}
	r1 := renderServiceRow(false, s1, 60)
	r2 := renderServiceRow(false, s2, 60)
	if w1, w2 := ansi.StringWidth(r1), ansi.StringWidth(r2); w1 != w2 {
		t.Fatalf("service rows of equal pane width should have equal display width, got %d vs %d", w1, w2)
	}
}

func TestFilterBar_ActiveShowsCursor(t *testing.T) {
	got := filterBar("beta", true)
	if !strings.HasSuffix(got, "▌") {
		t.Fatalf("active filter bar should end with cursor, got %q", got)
	}
}

func TestFilterBar_InactiveEmpty(t *testing.T) {
	if got := filterBar("", false); got != "" {
		t.Fatalf("empty inactive filter bar should render nothing, got %q", got)
	}
}

func TestViewport_ShowsWindowedRows(t *testing.T) {
	rows := []string{"a", "b", "c", "d", "e", "f"}
	scroll := 0
	got := viewport(rows, 2, 3, &scroll)
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestViewport_ScrollsDownAsCursorAdvances(t *testing.T) {
	rows := []string{"a", "b", "c", "d", "e"}
	scroll := 0
	viewport(rows, 4, 2, &scroll)
	if scroll != 3 {
		t.Fatalf("scroll should advance to keep cursor visible, got %d", scroll)
	}
}

func TestViewport_NoCursorPreservesManualScroll(t *testing.T) {
	// cursor < 0 marks a scroll surface with no selection (env / dumps tab /
	// app logs). The caller controls scroll via moveCursor; viewport must
	// not snap back to zero just because there's no cursor on the page.
	rows := []string{"a", "b", "c", "d", "e", "f"}
	scroll := 2
	got := viewport(rows, -1, 3, &scroll)
	if scroll != 2 {
		t.Fatalf("scroll should stay at 2 when cursor is -1, got %d", scroll)
	}
	want := []string{"c", "d", "e"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestViewport_NoCursorClampsScrollPastEnd(t *testing.T) {
	// If the caller pushes detailScroll beyond the last visible page,
	// viewport must clamp so we never render an all-blank pane.
	rows := []string{"a", "b", "c"}
	scroll := 99
	viewport(rows, -1, 2, &scroll)
	if scroll != 1 {
		t.Fatalf("scroll should clamp to len(rows)-height=1, got %d", scroll)
	}
}

// TestCountFailingWorkers_AggregatesAcrossSitesAndWorktrees pins the helper
// the header pill uses: every failed worker (built-in, custom, per-worktree)
// counts so the "press H to heal" hint never under-reports.
func TestCountFailingWorkers_AggregatesAcrossSitesAndWorktrees(t *testing.T) {
	snap := Snapshot{
		Sites: []siteinfo.EnrichedSite{
			{
				QueueFailing:    true, // +1
				ScheduleFailing: false,
				FrameworkWorkers: []siteinfo.WorkerInfo{
					{Name: "vite", Failing: true}, // +1
					{Name: "messenger", Failing: false},
				},
				Worktrees: []siteinfo.WorktreeInfo{
					{
						Branch: "feat-x",
						FrameworkWorkers: []siteinfo.WorkerInfo{
							{Name: "vite", Failing: true}, // +1
						},
					},
				},
			},
			{HorizonFailing: true}, // +1
		},
	}
	if got := countFailingWorkers(snap); got != 4 {
		t.Errorf("countFailingWorkers = %d, want 4", got)
	}
}

// TestCountFailingWorkers_ZeroWhenAllHealthy keeps the header clean when
// every worker is happy.
func TestCountFailingWorkers_ZeroWhenAllHealthy(t *testing.T) {
	snap := Snapshot{
		Sites: []siteinfo.EnrichedSite{
			{HasQueueWorker: true, QueueRunning: true},
			{HasHorizon: true, HorizonRunning: true},
		},
	}
	if got := countFailingWorkers(snap); got != 0 {
		t.Errorf("countFailingWorkers = %d, want 0", got)
	}
}
