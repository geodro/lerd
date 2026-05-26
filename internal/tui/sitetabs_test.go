package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/siteinfo"
)

func TestSiteTabsHeader_HighlightsActive(t *testing.T) {
	for _, tab := range []siteTab{tabSiteOverview, tabSiteEnv, tabSiteDumps, tabSiteAppLogs} {
		got := stripANSI(siteTabsHeader(tab))
		want := siteTabLabel(tab)
		if !strings.Contains(got, want) {
			t.Errorf("active=%v: expected label %q in %q", tab, want, got)
		}
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
	m.appendDump(DumpEntry{ID: "1", Site: "acme", Text: "alice"})
	m.appendDump(DumpEntry{ID: "2", Site: "other", Text: "bob"})
	m.appendDump(DumpEntry{ID: "3", Site: "acme", Text: "carol"})

	site := &siteinfo.EnrichedSite{Name: "acme"}
	lines := siteDumpsContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "alice") || !strings.Contains(joined, "carol") {
		t.Errorf("expected acme entries:\n%s", joined)
	}
	if strings.Contains(joined, "bob") {
		t.Errorf("expected other-site entry to be filtered out:\n%s", joined)
	}
	// Header now compares the matched count against site-scoped buffer
	// rather than total buffer (because chip/search filters are AND'd in
	// the new pipeline). Two acme entries / two acme entries in the
	// scoped slice = "2 of 2".
	if !strings.Contains(joined, "2 of 2 match this site") {
		t.Errorf("expected count summary '2 of 2 match this site':\n%s", joined)
	}
}

func TestSiteDumpsContent_EmptyShowsHint(t *testing.T) {
	m := NewModel("test")
	site := &siteinfo.EnrichedSite{Name: "acme"}
	lines := siteDumpsContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "no dumps from this site") {
		t.Errorf("expected empty-state hint:\n%s", joined)
	}
}

func TestSiteAppLogsContent_NoLogsShowsHint(t *testing.T) {
	m := NewModel("test")
	site := &siteinfo.EnrichedSite{Name: "acme", Path: t.TempDir()}
	lines := siteAppLogsContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "no app log paths declared") {
		t.Errorf("expected empty-state hint:\n%s", joined)
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{500, "500B"},
		{2048, "2KB"},
		{int64(5 * 1024 * 1024), "5.0MB"},
		{int64(2 * 1024 * 1024 * 1024), "2.0GB"},
	}
	for _, c := range cases {
		if got := humanSize(c.in); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
