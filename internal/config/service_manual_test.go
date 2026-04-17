package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
