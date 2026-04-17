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
	// Running bucket first, paused last.
	if got[0].State != stateRunning {
		t.Fatalf("running should sort first, got %v", got[0].State)
	}
	if got[len(got)-1].State != statePaused {
		t.Fatalf("paused should sort last, got %v", got[len(got)-1].State)
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
