package tui

import (
	"testing"

	"github.com/geodro/lerd/internal/siteinfo"
)

func sitesFixture() []siteinfo.EnrichedSite {
	return []siteinfo.EnrichedSite{
		{Name: "alpha", Domains: []string{"alpha.test"}, FrameworkLabel: "Laravel 11", FPMRunning: true},
		{Name: "beta", Domains: []string{"beta.test", "beta-admin.test"}, FrameworkLabel: "Symfony 7", Paused: true},
		{Name: "gamma", Domains: []string{"gamma.test"}, FrameworkLabel: "Laravel 11", FPMRunning: false},
	}
}

func TestFilterSites_ByDomain(t *testing.T) {
	got := filteredSortedSites(sitesFixture(), "admin", siteSortName)
	if len(got) != 1 || got[0].Name != "beta" {
		t.Fatalf("expected beta (matches beta-admin.test), got %+v", names(got))
	}
}

func TestFilterSites_ByFrameworkLabel(t *testing.T) {
	got := filteredSortedSites(sitesFixture(), "symfony", siteSortName)
	if len(got) != 1 || got[0].Name != "beta" {
		t.Fatalf("expected beta, got %+v", names(got))
	}
}

func TestFilterSites_CaseInsensitive(t *testing.T) {
	got := filteredSortedSites(sitesFixture(), "ALPHA", siteSortName)
	if len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("filter should be case-insensitive, got %+v", names(got))
	}
}

func TestSortSites_Framework(t *testing.T) {
	got := filteredSortedSites(sitesFixture(), "", siteSortFramework)
	// Laravel entries come before Symfony alphabetically.
	if got[0].FrameworkLabel != "Laravel 11" || got[len(got)-1].FrameworkLabel != "Symfony 7" {
		t.Fatalf("framework sort wrong: %+v", frameworkLabels(got))
	}
}

func TestSortSites_StatusBuckets(t *testing.T) {
	got := filteredSortedSites(sitesFixture(), "", siteSortStatus)
	if got[0].Name != "alpha" {
		t.Fatalf("running site should come first, got %s", got[0].Name)
	}
	if got[len(got)-1].Name != "beta" {
		t.Fatalf("paused site should come last, got %s", got[len(got)-1].Name)
	}
}

func servicesFixture() []ServiceRow {
	return []ServiceRow{
		{Name: "mysql", State: stateRunning, SiteCount: 3},
		{Name: "redis", State: stateStopped, SiteCount: 1},
		{Name: "mailpit", State: statePaused, SiteCount: 2},
		{Name: "custom-x", State: stateRunning, SiteCount: 0, Custom: true},
	}
}

func TestFilterServices_ByName(t *testing.T) {
	got := filteredSortedServices(servicesFixture(), "redis", svcSortName)
	if len(got) != 1 || got[0].Name != "redis" {
		t.Fatalf("expected redis, got %+v", svcNames(got))
	}
}

func TestSortServices_Usage(t *testing.T) {
	got := filteredSortedServices(servicesFixture(), "", svcSortUsage)
	// Highest site count first (mysql=3, mailpit=2, redis=1, custom-x=0).
	if got[0].Name != "mysql" || got[3].Name != "custom-x" {
		t.Fatalf("usage sort wrong: %+v", svcNames(got))
	}
}

func TestSortServices_Status(t *testing.T) {
	got := filteredSortedServices(servicesFixture(), "", svcSortStatus)
	// Within the Core group: running (mysql) first, paused (mailpit) last.
	// custom-x is in the Custom group so it lands after all Core entries
	// regardless of status; that's why we check the position of the Core
	// states explicitly rather than the global last element.
	if got[0].State != stateRunning || got[0].Name != "mysql" {
		t.Fatalf("running Core entry should sort first, got %v", got[0])
	}
	// Find the last Core-group entry and assert it's the paused one.
	lastCoreIdx := -1
	for i, s := range got {
		if classifyService(s) == groupCore {
			lastCoreIdx = i
		}
	}
	if lastCoreIdx < 0 || got[lastCoreIdx].State != statePaused {
		t.Fatalf("paused Core entry should sort last within Core, got %v", got[lastCoreIdx])
	}
}

func TestSortLabels(t *testing.T) {
	cases := []struct {
		mode  siteSortMode
		label string
	}{
		{siteSortName, "name"},
		{siteSortStatus, "status"},
		{siteSortFramework, "framework"},
	}
	for _, c := range cases {
		if got := c.mode.label(); got != c.label {
			t.Errorf("site %d label=%q want %q", c.mode, got, c.label)
		}
	}
	if svcSortUsage.label() != "usage" {
		t.Error("usage label")
	}
}

func frameworkLabels(ss []siteinfo.EnrichedSite) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.FrameworkLabel
	}
	return out
}

func svcNames(ss []ServiceRow) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name
	}
	return out
}

func TestGroupSecondariesUnderMains(t *testing.T) {
	list := []siteinfo.EnrichedSite{
		{Name: "admin-starlane", Domains: []string{"admin.starlane.test"}, Group: "starlane", GroupSubdomain: "admin"},
		{Name: "starlane", Domains: []string{"starlane.test"}, Group: "starlane"},
		{Name: "blog", Domains: []string{"blog.test"}},
	}
	got := groupSecondariesUnderMains(list)
	want := []string{"starlane", "admin-starlane", "blog"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, n := range want {
		if got[i].Name != n {
			t.Errorf("pos %d = %q, want %q (full: %v)", i, got[i].Name, n, names(got))
		}
	}
}

func TestGroupSecondariesUnderMains_orphanKeepsPlace(t *testing.T) {
	// Secondary whose main is absent (e.g. filtered out) must still appear.
	list := []siteinfo.EnrichedSite{
		{Name: "admin-starlane", Domains: []string{"admin.starlane.test"}, Group: "starlane", GroupSubdomain: "admin"},
		{Name: "blog", Domains: []string{"blog.test"}},
	}
	got := groupSecondariesUnderMains(list)
	if len(got) != 2 {
		t.Fatalf("orphan secondary dropped: %v", names(got))
	}
}
