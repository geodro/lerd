package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/dumps"
	"github.com/geodro/lerd/internal/siteinfo"
)

func TestSiteTabsHeader_HighlightsActive(t *testing.T) {
	base := []siteTab{tabSiteOverview, tabSiteEnv, tabSiteDebug}
	for _, tab := range base {
		got := stripANSI(siteTabsHeader(tab, base))
		want := siteTabLabel(tab)
		if !strings.Contains(got, want) {
			t.Errorf("active=%v: expected label %q in %q", tab, want, got)
		}
	}
}

func TestAvailableSiteTabs_DoctorForEveryFramework(t *testing.T) {
	plain := availableSiteTabs(&siteinfo.EnrichedSite{Name: "static", FrameworkName: "wordpress"})
	if !slices.Contains(plain, tabSiteDoctor) {
		t.Errorf("every framework should offer the Doctor tab, got %v", plain)
	}
	laravel := availableSiteTabs(&siteinfo.EnrichedSite{Name: "app", FrameworkName: "laravel"})
	if !slices.Contains(laravel, tabSiteDoctor) {
		t.Errorf("Laravel site should offer the Doctor tab, got %v", laravel)
	}
	// Doctor is the fourth tab, so the strip numbers it [4].
	if got := stripANSI(siteTabsHeader(tabSiteOverview, laravel)); !strings.Contains(got, "[4] Doctor") {
		t.Errorf("strip should carry [4] Doctor, got %q", got)
	}
}

func TestSiteEnvContent_ShowsFileContents(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_KEY=abc\nDB_PASS=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewModel("test")
	site := &siteinfo.EnrichedSite{Name: "acme", Path: dir}
	lines := siteEnvContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "APP_KEY=abc") || !strings.Contains(joined, "DB_PASS=secret") {
		t.Errorf("expected env contents in output:\n%s", joined)
	}
}

func TestSiteEnvContent_MissingFileShowsHint(t *testing.T) {
	m := NewModel("test")
	site := &siteinfo.EnrichedSite{Name: "acme", Path: t.TempDir()}
	lines := siteEnvContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "no .env on disk") {
		t.Errorf("expected missing-env hint:\n%s", joined)
	}
}

func TestSiteEnvContent_EmptyFileShowsHint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewModel("test")
	site := &siteinfo.EnrichedSite{Name: "acme", Path: dir}
	lines := siteEnvContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "empty") {
		t.Errorf("expected empty-env hint:\n%s", joined)
	}
}

func TestSiteDumpsContent_FiltersToFocusedSite(t *testing.T) {
	m := NewModel("test")
	m.appendDebug(dumpEv(DumpEntry{ID: "1", Site: "acme", Text: "alice"}))
	m.appendDebug(dumpEv(DumpEntry{ID: "2", Site: "other", Text: "bob"}))
	m.appendDebug(dumpEv(DumpEntry{ID: "3", Site: "acme", Text: "carol"}))

	site := &siteinfo.EnrichedSite{Name: "acme"}
	// debugLens defaults to the Dumps lens, so this exercises the dump path
	// of the per-site Debug tab.
	lines := siteDebugContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "alice") || !strings.Contains(joined, "carol") {
		t.Errorf("expected acme entries:\n%s", joined)
	}
	if strings.Contains(joined, "bob") {
		t.Errorf("expected other-site entry to be filtered out:\n%s", joined)
	}
	// Two acme dumps shown out of two acme dumps buffered (site-scoped count).
	if !strings.Contains(joined, "2 shown / 2 buffered") {
		t.Errorf("expected site-scoped count '2 shown / 2 buffered':\n%s", joined)
	}
}

func TestSiteDebugContent_QueryLensScopesToSite(t *testing.T) {
	m := NewModel("test")
	setLens(m, dumps.KindQuery)
	m.appendDebug(qEv("1", "r1", "select * from acme_orders", 250))
	// A query from a different site must not leak into acme's Debug tab.
	other := qEv("2", "r2", "select * from other_table", 2)
	other.Ctx.Site = "other"
	m.appendDebug(other)

	site := &siteinfo.EnrichedSite{Name: "acme"}
	joined := stripANSI(strings.Join(siteDebugContentLines(m, site, 120), "\n"))
	if !strings.Contains(joined, "Debug for acme") {
		t.Errorf("expected per-site Debug header:\n%s", joined)
	}
	if !strings.Contains(joined, "select * from acme_orders") || !strings.Contains(joined, "slow") {
		t.Errorf("expected this site's slow query to render:\n%s", joined)
	}
	if strings.Contains(joined, "other_table") {
		t.Errorf("another site's query leaked into the tab:\n%s", joined)
	}
}

func TestSiteDumpsContent_EmptyShowsHint(t *testing.T) {
	m := NewModel("test")
	site := &siteinfo.EnrichedSite{Name: "acme"}
	lines := siteDebugContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "no dumps from this site") {
		t.Errorf("expected empty-state hint:\n%s", joined)
	}
}
