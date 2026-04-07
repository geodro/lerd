package config

import (
	"os"
	"testing"
)

// setDataDir points DataDir() (and SitesFile()) at a temp directory.
func setDataDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// ── AddSite / LoadSites ───────────────────────────────────────────────────────

func TestAddSite_Basic(t *testing.T) {
	setDataDir(t)

	site := Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/srv/myapp"}
	if err := AddSite(site); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	reg, err := LoadSites()
	if err != nil {
		t.Fatalf("LoadSites: %v", err)
	}
	if len(reg.Sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(reg.Sites))
	}
	if reg.Sites[0].Name != "myapp" {
		t.Errorf("Name = %q, want myapp", reg.Sites[0].Name)
	}
}

func TestAddSite_UpdateExisting(t *testing.T) {
	setDataDir(t)

	if err := AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/old"}); err != nil {
		t.Fatal(err)
	}
	if err := AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/new"}); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Sites) != 1 {
		t.Fatalf("expected 1 site after update, got %d", len(reg.Sites))
	}
	if reg.Sites[0].Path != "/new" {
		t.Errorf("Path = %q, want /new", reg.Sites[0].Path)
	}
}

// ── RemoveSite ────────────────────────────────────────────────────────────────

func TestRemoveSite(t *testing.T) {
	setDataDir(t)

	AddSite(Site{Name: "alpha", Domains: []string{"alpha.test"}, Path: "/alpha"})
	AddSite(Site{Name: "beta", Domains: []string{"beta.test"}, Path: "/beta"})

	if err := RemoveSite("alpha"); err != nil {
		t.Fatalf("RemoveSite: %v", err)
	}

	reg, err := LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Sites) != 1 || reg.Sites[0].Name != "beta" {
		t.Errorf("expected only beta after remove, got %v", reg.Sites)
	}
}

func TestRemoveSite_NotFound_NoError(t *testing.T) {
	setDataDir(t)

	if err := RemoveSite("ghost"); err != nil {
		t.Errorf("expected no error removing non-existent site, got: %v", err)
	}
}

// ── FindSite ─────────────────────────────────────────────────────────────────

func TestFindSite_ByName(t *testing.T) {
	setDataDir(t)
	AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/srv/myapp"})

	s, err := FindSite("myapp")
	if err != nil {
		t.Fatalf("FindSite: %v", err)
	}
	if s.PrimaryDomain() != "myapp.test" {
		t.Errorf("PrimaryDomain() = %q, want myapp.test", s.PrimaryDomain())
	}
}

func TestFindSite_NotFound(t *testing.T) {
	setDataDir(t)

	_, err := FindSite("ghost")
	if err == nil {
		t.Error("expected error for missing site")
	}
}

func TestFindSiteByPath(t *testing.T) {
	setDataDir(t)
	AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/srv/myapp"})

	s, err := FindSiteByPath("/srv/myapp")
	if err != nil {
		t.Fatalf("FindSiteByPath: %v", err)
	}
	if s.Name != "myapp" {
		t.Errorf("Name = %q, want myapp", s.Name)
	}
}

func TestFindSiteByPath_NotFound(t *testing.T) {
	setDataDir(t)

	_, err := FindSiteByPath("/nonexistent")
	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestFindSiteByDomain(t *testing.T) {
	setDataDir(t)
	AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/srv/myapp"})

	s, err := FindSiteByDomain("myapp.test")
	if err != nil {
		t.Fatalf("FindSiteByDomain: %v", err)
	}
	if s.Path != "/srv/myapp" {
		t.Errorf("Path = %q, want /srv/myapp", s.Path)
	}
}

func TestFindSiteByDomain_MultiDomain(t *testing.T) {
	setDataDir(t)
	AddSite(Site{Name: "myapp", Domains: []string{"myapp.test", "api.test"}, Path: "/srv/myapp"})

	// Should find by secondary domain too
	s, err := FindSiteByDomain("api.test")
	if err != nil {
		t.Fatalf("FindSiteByDomain (secondary): %v", err)
	}
	if s.Name != "myapp" {
		t.Errorf("Name = %q, want myapp", s.Name)
	}
}

func TestFindSiteByDomain_NotFound(t *testing.T) {
	setDataDir(t)

	_, err := FindSiteByDomain("ghost.test")
	if err == nil {
		t.Error("expected error for missing domain")
	}
}

// ── IsDomainUsed ─────────────────────────────────────────────────────────────

func TestIsDomainUsed(t *testing.T) {
	setDataDir(t)
	AddSite(Site{Name: "myapp", Domains: []string{"myapp.test", "api.test"}, Path: "/srv/myapp"})

	s, err := IsDomainUsed("api.test")
	if err != nil {
		t.Fatalf("IsDomainUsed: %v", err)
	}
	if s == nil {
		t.Fatal("expected site, got nil")
	}
	if s.Name != "myapp" {
		t.Errorf("Name = %q, want myapp", s.Name)
	}

	s, err = IsDomainUsed("free.test")
	if err != nil {
		t.Fatalf("IsDomainUsed: %v", err)
	}
	if s != nil {
		t.Errorf("expected nil for free domain, got %+v", s)
	}
}

// ── IgnoreSite ────────────────────────────────────────────────────────────────

func TestIgnoreSite(t *testing.T) {
	setDataDir(t)
	AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/srv/myapp"})

	if err := IgnoreSite("myapp"); err != nil {
		t.Fatalf("IgnoreSite: %v", err)
	}

	s, err := FindSite("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Ignored {
		t.Error("expected site to be marked Ignored")
	}
}

func TestIgnoreSite_NotFound(t *testing.T) {
	setDataDir(t)

	err := IgnoreSite("ghost")
	if err == nil {
		t.Error("expected error when ignoring non-existent site")
	}
}

// ── SaveSites / LoadSites round-trip ─────────────────────────────────────────

func TestSaveLoad_RoundTrip(t *testing.T) {
	setDataDir(t)

	reg := &SiteRegistry{
		Sites: []Site{
			{Name: "alpha", Domains: []string{"alpha.test"}, Path: "/alpha", PHPVersion: "8.3", Secured: true},
			{Name: "beta", Domains: []string{"beta.test", "api.test"}, Path: "/beta", PHPVersion: "8.4"},
		},
	}
	if err := SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	got, err := LoadSites()
	if err != nil {
		t.Fatalf("LoadSites: %v", err)
	}
	if len(got.Sites) != 2 {
		t.Fatalf("expected 2 sites, got %d", len(got.Sites))
	}
	if got.Sites[0].PHPVersion != "8.3" || !got.Sites[0].Secured {
		t.Errorf("alpha not persisted correctly: %+v", got.Sites[0])
	}
	if got.Sites[1].PHPVersion != "8.4" {
		t.Errorf("beta not persisted correctly: %+v", got.Sites[1])
	}
	if len(got.Sites[1].Domains) != 2 || got.Sites[1].Domains[1] != "api.test" {
		t.Errorf("beta domains not persisted correctly: %v", got.Sites[1].Domains)
	}
}

func TestLoadSites_EmptyWhenMissing(t *testing.T) {
	setDataDir(t)

	reg, err := LoadSites()
	if err != nil {
		t.Fatalf("LoadSites on missing file: %v", err)
	}
	if len(reg.Sites) != 0 {
		t.Errorf("expected empty registry, got %v", reg.Sites)
	}
}

// ── Legacy domain field ─────────────────────────────────────────────────────

func TestLoadSites_LegacyDomainField(t *testing.T) {
	setDataDir(t)

	// Simulate a legacy sites.yaml with single "domain" field
	if err := os.MkdirAll(DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	yamlData := `sites:
- name: legacy
  domain: legacy.test
  path: /srv/legacy
  php_version: "8.4"
  node_version: "22"
  secured: false
`
	if err := os.WriteFile(SitesFile(), []byte(yamlData), 0644); err != nil {
		t.Fatal(err)
	}

	reg, err := LoadSites()
	if err != nil {
		t.Fatalf("LoadSites: %v", err)
	}
	if len(reg.Sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(reg.Sites))
	}
	if reg.Sites[0].PrimaryDomain() != "legacy.test" {
		t.Errorf("PrimaryDomain() = %q, want legacy.test", reg.Sites[0].PrimaryDomain())
	}
	if len(reg.Sites[0].Domains) != 1 {
		t.Errorf("expected 1 domain from legacy field, got %d", len(reg.Sites[0].Domains))
	}
}

// ── PrimaryDomain ───────────────────────────────────────────────────────────

func TestPrimaryDomain(t *testing.T) {
	s := Site{Domains: []string{"myapp.test", "api.test"}}
	if got := s.PrimaryDomain(); got != "myapp.test" {
		t.Errorf("PrimaryDomain() = %q, want myapp.test", got)
	}
}

func TestPrimaryDomain_empty(t *testing.T) {
	s := Site{}
	if got := s.PrimaryDomain(); got != "" {
		t.Errorf("PrimaryDomain() = %q, want empty", got)
	}
}

// ── HasDomain ────────────────────────────────────────────────────────────────

func TestHasDomain(t *testing.T) {
	s := Site{Domains: []string{"myapp.test", "api.test"}}
	if !s.HasDomain("api.test") {
		t.Error("expected HasDomain(api.test) = true")
	}
	if s.HasDomain("other.test") {
		t.Error("expected HasDomain(other.test) = false")
	}
}

// ── IsDomainUsed ─────────────────────────────────────────────────────────────

func TestIsDomainUsed_free(t *testing.T) {
	setDataDir(t)
	AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/srv/myapp"})

	s, err := IsDomainUsed("free.test")
	if err != nil {
		t.Fatal(err)
	}
	if s != nil {
		t.Errorf("expected nil for free domain, got %+v", s)
	}
}

func TestIsDomainUsed_taken(t *testing.T) {
	setDataDir(t)
	AddSite(Site{Name: "myapp", Domains: []string{"myapp.test", "api.test"}, Path: "/srv/myapp"})

	s, err := IsDomainUsed("api.test")
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Name != "myapp" {
		t.Errorf("expected myapp, got %v", s)
	}
}

// ── FindSiteByDomain with multiple domains ──────────────────────────────────

func TestFindSiteByDomain_secondaryDomain(t *testing.T) {
	setDataDir(t)
	AddSite(Site{Name: "myapp", Domains: []string{"myapp.test", "api.test", "admin.test"}, Path: "/srv/myapp"})

	for _, domain := range []string{"myapp.test", "api.test", "admin.test"} {
		s, err := FindSiteByDomain(domain)
		if err != nil {
			t.Fatalf("FindSiteByDomain(%q): %v", domain, err)
		}
		if s.Name != "myapp" {
			t.Errorf("FindSiteByDomain(%q).Name = %q, want myapp", domain, s.Name)
		}
	}
}

// ── Save/Load round-trip with multiple domains ──────────────────────────────

func TestSaveLoad_MultiDomain_RoundTrip(t *testing.T) {
	setDataDir(t)

	reg := &SiteRegistry{
		Sites: []Site{
			{Name: "multi", Domains: []string{"multi.test", "api.test", "admin.test"}, Path: "/multi", PHPVersion: "8.4"},
		},
	}
	if err := SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	got, err := LoadSites()
	if err != nil {
		t.Fatalf("LoadSites: %v", err)
	}
	if len(got.Sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(got.Sites))
	}
	if len(got.Sites[0].Domains) != 3 {
		t.Fatalf("expected 3 domains, got %d: %v", len(got.Sites[0].Domains), got.Sites[0].Domains)
	}
	if got.Sites[0].Domains[0] != "multi.test" {
		t.Errorf("Domains[0] = %q, want multi.test", got.Sites[0].Domains[0])
	}
	if got.Sites[0].Domains[2] != "admin.test" {
		t.Errorf("Domains[2] = %q, want admin.test", got.Sites[0].Domains[2])
	}
}

// ── Legacy domain field is not written back ─────────────────────────────────

func TestSaveLoad_LegacyMigration(t *testing.T) {
	setDataDir(t)

	// Write legacy format
	if err := os.MkdirAll(DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(SitesFile(), []byte(`sites:
- name: old
  domain: old.test
  path: /old
  php_version: "8.3"
  node_version: "22"
  secured: false
`), 0644)

	// Load (reads legacy domain), then save (writes domains array)
	reg, err := LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	// Re-load and verify it uses domains array
	reg2, err := LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg2.Sites[0].Domains) != 1 || reg2.Sites[0].Domains[0] != "old.test" {
		t.Errorf("after migration: Domains = %v", reg2.Sites[0].Domains)
	}

	// Verify the YAML file no longer contains the old "domain:" key
	data, _ := os.ReadFile(SitesFile())
	yaml := string(data)
	if contains(yaml, "domain: old.test") {
		t.Error("saved YAML still contains legacy 'domain:' field")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
