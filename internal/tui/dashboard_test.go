package tui

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/siteinfo"
	"github.com/geodro/lerd/internal/stats"
)

// TestDashboardContent_RendersAllSections ensures every promised section
// header is present so the dashboard never silently loses a widget after a
// refactor.
func TestDashboardContent_RendersAllSections(t *testing.T) {
	m := NewModel("test")
	lines, _ := dashboardContentLinesWithCursor(m, false, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	for _, want := range []string{"Dashboard", "Overview", "System health", "Resources", "Lerd"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing section %q in dashboard:\n%s", want, joined)
		}
	}
}

// TestDashboardContent_ShowsHealthyWhenNoFailures verifies the hero line
// reflects the heal state so users see the positive signal even when the
// header banner is hidden.
func TestDashboardContent_ShowsHealthyWhenNoFailures(t *testing.T) {
	m := NewModel("test")
	lines, _ := dashboardContentLinesWithCursor(m, false, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "all workers healthy") {
		t.Errorf("expected healthy hero with no failing workers:\n%s", joined)
	}
}

// TestDashboardContent_ShowsFailingWorkerCount counts failing workers from
// the snapshot so the dashboard summary always matches the heal hint in the
// header.
func TestDashboardContent_ShowsFailingWorkerCount(t *testing.T) {
	m := NewModel("test")
	m.snap.Sites = []siteinfo.EnrichedSite{
		{Name: "a", QueueFailing: true, HasQueueWorker: true},
		{Name: "b", ScheduleFailing: true, HasScheduleWorker: true},
	}
	lines, _ := dashboardContentLinesWithCursor(m, false, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "2 workers failing") {
		t.Errorf("expected '2 workers failing':\n%s", joined)
	}
	if !strings.Contains(joined, "press H") {
		t.Errorf("dashboard should hint at H to heal:\n%s", joined)
	}
}

// TestDashboardContent_ShowsStatsWhenAvailable verifies the resources block
// renders concrete numbers from the cached snapshot, not the placeholder.
func TestDashboardContent_ShowsStatsWhenAvailable(t *testing.T) {
	m := NewModel("test")
	m.stats = stats.Snapshot{
		Available:       true,
		TotalCPUPercent: 12.5,
		TotalMemBytes:   128 * 1024 * 1024,
		HostMemBytes:    32 * 1024 * 1024 * 1024,
		Containers: []stats.ContainerStat{
			{Name: "lerd-mysql", CPUPercent: 5.5, MemBytes: 100 * 1024 * 1024},
			{Name: "lerd-redis", CPUPercent: 1.0, MemBytes: 28 * 1024 * 1024},
		},
	}
	lines, _ := dashboardContentLinesWithCursor(m, false, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "12.5%") {
		t.Errorf("expected '12.5%%' total CPU:\n%s", joined)
	}
	if !strings.Contains(joined, "lerd-mysql") {
		t.Errorf("expected top container 'lerd-mysql':\n%s", joined)
	}
	if strings.Contains(joined, "collecting") {
		t.Errorf("should not show placeholder when Available=true:\n%s", joined)
	}
}

// TestDashboardContent_PlaceholderWhenStatsCollecting renders the polite
// "collecting…" message during the first window before the poller has run.
func TestDashboardContent_PlaceholderWhenStatsCollecting(t *testing.T) {
	m := NewModel("test")
	// stats zero-valued: Available=false
	lines, _ := dashboardContentLinesWithCursor(m, false, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "collecting") {
		t.Errorf("expected 'collecting…' placeholder when stats unavailable:\n%s", joined)
	}
}

// stripANSI removes lipgloss escape sequences so tests can assert against
// the visible characters without coupling to the colour palette.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
