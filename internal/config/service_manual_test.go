package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ServicePublishedPort reflects a shifted published port (set by `lerd service
// port` or the port-ownership guard) so host-facing surfaces — e.g. the
// dashboard's connection URL — target where lerd's container actually listens,
// not the engine default a coexisting host server may own.
func TestServicePublishedPort(t *testing.T) {
	setConfigDir(t)

	// No override → the "use default" sentinel 0.
	if got := ServicePublishedPort("postgres"); got != 0 {
		t.Errorf("ServicePublishedPort(postgres) with no override = %d, want 0", got)
	}

	// Guard auto-shifts lerd-postgres off a host-owned :5432 onto :5434.
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.Services == nil {
		cfg.Services = map[string]ServiceConfig{}
	}
	cfg.Services["postgres"] = ServiceConfig{Enabled: true, Port: 5432, PublishedPort: 5434}
	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	if got := ServicePublishedPort("postgres"); got != 5434 {
		t.Errorf("ServicePublishedPort(postgres) after shift = %d, want 5434", got)
	}

	// Unknown service → 0 (never panics on a missing entry).
	if got := ServicePublishedPort("does-not-exist"); got != 0 {
		t.Errorf("ServicePublishedPort(unknown) = %d, want 0", got)
	}
}

func TestCountSitesUsingService_LerdYAML(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	lerdYAML := `services:
  - postgres
`
	os.WriteFile(filepath.Join(siteDir, ".lerd.yaml"), []byte(lerdYAML), 0644)

	if err := AddSite(Site{
		Name:    "goapp",
		Domains: []string{"goapp.test"},
		Path:    siteDir,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("postgres")
	if count != 1 {
		t.Errorf("CountSitesUsingService(postgres) = %d, want 1", count)
	}

	count = CountSitesUsingService("mysql")
	if count != 0 {
		t.Errorf("CountSitesUsingService(mysql) = %d, want 0", count)
	}
}

func TestCountSitesUsingService_NoEnvNoYAML(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	if err := AddSite(Site{
		Name:    "bare",
		Domains: []string{"bare.test"},
		Path:    siteDir,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("postgres")
	if count != 0 {
		t.Errorf("CountSitesUsingService(postgres) = %d, want 0", count)
	}
}

func TestCountSitesUsingService_EnvFallback(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	os.WriteFile(filepath.Join(siteDir, ".env"), []byte("DB_HOST=lerd-mysql\n"), 0644)

	if err := AddSite(Site{
		Name:    "phpapp",
		Domains: []string{"phpapp.test"},
		Path:    siteDir,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("mysql")
	if count != 1 {
		t.Errorf("CountSitesUsingService(mysql) = %d, want 1", count)
	}
}

func TestCountSitesUsingService_LerdYAMLTakesPriority(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	// .lerd.yaml declares postgres, .env also references lerd-mysql.
	// The function should count the site for postgres via .lerd.yaml and
	// NOT double-count it (goto next after .lerd.yaml match).
	lerdYAML := `services:
  - postgres
`
	os.WriteFile(filepath.Join(siteDir, ".lerd.yaml"), []byte(lerdYAML), 0644)
	os.WriteFile(filepath.Join(siteDir, ".env"), []byte("DB_HOST=lerd-postgres\n"), 0644)

	if err := AddSite(Site{
		Name:    "dualapp",
		Domains: []string{"dualapp.test"},
		Path:    siteDir,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("postgres")
	if count != 1 {
		t.Errorf("CountSitesUsingService(postgres) = %d, want 1 (not double-counted)", count)
	}
}

func TestCountSitesUsingService_IgnoredSiteSkipped(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	lerdYAML := `services:
  - redis
`
	os.WriteFile(filepath.Join(siteDir, ".lerd.yaml"), []byte(lerdYAML), 0644)

	if err := AddSite(Site{
		Name:    "ignored",
		Domains: []string{"ignored.test"},
		Path:    siteDir,
		Ignored: true,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("redis")
	if count != 0 {
		t.Errorf("CountSitesUsingService(redis) = %d, want 0 for ignored site", count)
	}
}

func TestCountSitesUsingService_PausedSiteSkipped(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	lerdYAML := `services:
  - redis
`
	os.WriteFile(filepath.Join(siteDir, ".lerd.yaml"), []byte(lerdYAML), 0644)

	if err := AddSite(Site{
		Name:    "paused",
		Domains: []string{"paused.test"},
		Path:    siteDir,
		Paused:  true,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("redis")
	if count != 0 {
		t.Errorf("CountSitesUsingService(redis) = %d, want 0 for paused site", count)
	}
}

func TestSitesUsingService_ReturnsLinkedSites(t *testing.T) {
	setDataDir(t)

	yamlSiteDir := t.TempDir()
	os.WriteFile(filepath.Join(yamlSiteDir, ".lerd.yaml"), []byte("services:\n  - mariadb\n"), 0644)
	if err := AddSite(Site{Name: "yaml-site", Domains: []string{"yaml.test"}, Path: yamlSiteDir}); err != nil {
		t.Fatalf("AddSite yaml: %v", err)
	}

	envSiteDir := t.TempDir()
	os.WriteFile(filepath.Join(envSiteDir, ".env"), []byte("DB_HOST=lerd-mariadb\n"), 0644)
	if err := AddSite(Site{Name: "env-site", Domains: []string{"env.test"}, Path: envSiteDir}); err != nil {
		t.Fatalf("AddSite env: %v", err)
	}

	otherSiteDir := t.TempDir()
	os.WriteFile(filepath.Join(otherSiteDir, ".env"), []byte("DB_HOST=lerd-postgres\n"), 0644)
	if err := AddSite(Site{Name: "other-site", Domains: []string{"other.test"}, Path: otherSiteDir}); err != nil {
		t.Fatalf("AddSite other: %v", err)
	}

	sites := SitesUsingService("mariadb")
	if len(sites) != 2 {
		t.Fatalf("SitesUsingService(mariadb) returned %d sites, want 2", len(sites))
	}
	names := map[string]bool{}
	for _, s := range sites {
		names[s.Name] = true
	}
	if !names["yaml-site"] || !names["env-site"] {
		t.Errorf("expected both yaml-site and env-site, got %v", names)
	}
	if names["other-site"] {
		t.Errorf("other-site (uses postgres) should not be in mariadb result")
	}
}

func TestSitesUsingService_SkipsIgnoredAndPaused(t *testing.T) {
	setDataDir(t)

	activeDir := t.TempDir()
	os.WriteFile(filepath.Join(activeDir, ".lerd.yaml"), []byte("services:\n  - redis\n"), 0644)
	if err := AddSite(Site{Name: "active", Domains: []string{"active.test"}, Path: activeDir}); err != nil {
		t.Fatalf("AddSite active: %v", err)
	}

	ignoredDir := t.TempDir()
	os.WriteFile(filepath.Join(ignoredDir, ".lerd.yaml"), []byte("services:\n  - redis\n"), 0644)
	if err := AddSite(Site{Name: "ignored", Domains: []string{"i.test"}, Path: ignoredDir, Ignored: true}); err != nil {
		t.Fatalf("AddSite ignored: %v", err)
	}

	pausedDir := t.TempDir()
	os.WriteFile(filepath.Join(pausedDir, ".lerd.yaml"), []byte("services:\n  - redis\n"), 0644)
	if err := AddSite(Site{Name: "paused", Domains: []string{"p.test"}, Path: pausedDir, Paused: true}); err != nil {
		t.Fatalf("AddSite paused: %v", err)
	}

	sites := SitesUsingService("redis")
	if len(sites) != 1 || sites[0].Name != "active" {
		t.Errorf("SitesUsingService(redis) = %v, want only [active]", sites)
	}
}

func TestSitesUsingService_LerdYAMLTakesPriorityOverEnv(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	os.WriteFile(filepath.Join(siteDir, ".lerd.yaml"), []byte("services:\n  - postgres\n"), 0644)
	os.WriteFile(filepath.Join(siteDir, ".env"), []byte("DB_HOST=lerd-postgres\n"), 0644)
	if err := AddSite(Site{Name: "dual", Domains: []string{"d.test"}, Path: siteDir}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	sites := SitesUsingService("postgres")
	if len(sites) != 1 {
		t.Errorf("SitesUsingService(postgres) returned %d sites, want 1 (no double-counting)", len(sites))
	}
}
