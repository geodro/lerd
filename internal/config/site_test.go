package config

import (
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

	site := Site{Name: "myapp", Domain: "myapp.test", Path: "/srv/myapp"}
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

	if err := AddSite(Site{Name: "myapp", Domain: "myapp.test", Path: "/old"}); err != nil {
		t.Fatal(err)
	}
	if err := AddSite(Site{Name: "myapp", Domain: "myapp.test", Path: "/new"}); err != nil {
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

	AddSite(Site{Name: "alpha", Domain: "alpha.test", Path: "/alpha"})
	AddSite(Site{Name: "beta", Domain: "beta.test", Path: "/beta"})

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
	AddSite(Site{Name: "myapp", Domain: "myapp.test", Path: "/srv/myapp"})

	s, err := FindSite("myapp")
	if err != nil {
		t.Fatalf("FindSite: %v", err)
	}
	if s.Domain != "myapp.test" {
		t.Errorf("Domain = %q, want myapp.test", s.Domain)
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
	AddSite(Site{Name: "myapp", Domain: "myapp.test", Path: "/srv/myapp"})

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
	AddSite(Site{Name: "myapp", Domain: "myapp.test", Path: "/srv/myapp"})

	s, err := FindSiteByDomain("myapp.test")
	if err != nil {
		t.Fatalf("FindSiteByDomain: %v", err)
	}
	if s.Path != "/srv/myapp" {
		t.Errorf("Path = %q, want /srv/myapp", s.Path)
	}
}

func TestFindSiteByDomain_NotFound(t *testing.T) {
	setDataDir(t)

	_, err := FindSiteByDomain("ghost.test")
	if err == nil {
		t.Error("expected error for missing domain")
	}
}

// ── IgnoreSite ────────────────────────────────────────────────────────────────

func TestIgnoreSite(t *testing.T) {
	setDataDir(t)
	AddSite(Site{Name: "myapp", Domain: "myapp.test", Path: "/srv/myapp"})

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
			{Name: "alpha", Domain: "alpha.test", Path: "/alpha", PHPVersion: "8.3", Secured: true},
			{Name: "beta", Domain: "beta.test", Path: "/beta", PHPVersion: "8.4"},
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

// ── IsLaravel ─────────────────────────────────────────────────────────────────

func TestIsLaravel(t *testing.T) {
	cases := []struct {
		framework string
		want      bool
	}{
		{"laravel", true},
		{"symfony", false},
		{"", false},
	}
	for _, c := range cases {
		s := Site{Framework: c.framework}
		if got := s.IsLaravel(); got != c.want {
			t.Errorf("IsLaravel() for framework=%q = %v, want %v", c.framework, got, c.want)
		}
	}
}
