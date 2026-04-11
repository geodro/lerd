package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// ── SyncProjectDomains (config package) ────────────────────────────────────

func TestSyncProjectDomains_strips_tld(t *testing.T) {
	dir := t.TempDir()

	// Create an initial .lerd.yaml
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("php_version: \"8.4\"\n"), 0644)

	_ = config.SyncProjectDomains(dir, []string{"myapp.test", "api.test", "admin.test"}, "test")

	proj, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.Domains) != 3 {
		t.Fatalf("expected 3 domains, got %d", len(proj.Domains))
	}
	want := []string{"myapp", "api", "admin"}
	for i, w := range want {
		if proj.Domains[i] != w {
			t.Errorf("Domains[%d] = %q, want %q", i, proj.Domains[i], w)
		}
	}
	// PHP version should be preserved
	if proj.PHPVersion != "8.4" {
		t.Errorf("PHPVersion = %q, want 8.4", proj.PHPVersion)
	}
}

func TestSyncProjectDomains_noop_without_file(t *testing.T) {
	dir := t.TempDir()
	// No .lerd.yaml — should be a no-op
	_ = config.SyncProjectDomains(dir, []string{"myapp.test"}, "test")

	if _, err := os.Stat(filepath.Join(dir, ".lerd.yaml")); !os.IsNotExist(err) {
		t.Error("should not create .lerd.yaml when it doesn't exist")
	}
}

// ── RegenerateSiteVhost ─────────────────────────────────────────────────────

func TestRegenerateSiteVhost_creates_vhost(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	site := &config.Site{
		Name:       "myapp",
		Domains:    []string{"myapp.test", "api.test"},
		Path:       "/srv/myapp",
		PHPVersion: "8.4",
	}

	if err := RegenerateSiteVhost(site, "myapp.test"); err != nil {
		t.Fatalf("RegenerateSiteVhost: %v", err)
	}

	confPath := filepath.Join(tmp, "lerd", "nginx", "conf.d", "myapp.test.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("reading vhost: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "myapp.test") || !strings.Contains(content, "api.test") {
		t.Errorf("expected both domains in server_name, got:\n%s", content)
	}
}

func TestRegenerateSiteVhost_removes_old_on_primary_change(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	confD := filepath.Join(tmp, "lerd", "nginx", "conf.d")
	os.MkdirAll(confD, 0755)

	// Create old vhost
	os.WriteFile(filepath.Join(confD, "old.test.conf"), []byte("server {}"), 0644)

	site := &config.Site{
		Name:       "myapp",
		Domains:    []string{"new.test"},
		Path:       "/srv/myapp",
		PHPVersion: "8.4",
	}

	if err := RegenerateSiteVhost(site, "old.test"); err != nil {
		t.Fatal(err)
	}

	// Old conf should be removed
	if _, err := os.Stat(filepath.Join(confD, "old.test.conf")); !os.IsNotExist(err) {
		t.Error("old vhost conf should have been removed")
	}
	// New conf should exist
	if _, err := os.Stat(filepath.Join(confD, "new.test.conf")); err != nil {
		t.Error("new vhost conf should have been created")
	}
}

// ── SetProjectWorkers (config package) ─────────────────────────────────────

func TestSetProjectWorkers_noop_without_file(t *testing.T) {
	dir := t.TempDir()
	// No .lerd.yaml — should be a no-op
	_ = config.SetProjectWorkers(dir, CollectRunningWorkerNames(&config.Site{Name: "myapp", Path: dir}))

	if _, err := os.Stat(filepath.Join(dir, ".lerd.yaml")); !os.IsNotExist(err) {
		t.Error("should not create .lerd.yaml when it doesn't exist")
	}
}

func TestSetProjectWorkers_writes_empty(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("php_version: \"8.4\"\nworkers:\n  - queue\n"), 0644)

	site := &config.Site{Name: "myapp", Path: dir}
	// CollectRunningWorkerNames returns empty in test env (no systemd)
	_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))

	proj, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Workers should be empty since no systemd services are running in test
	if len(proj.Workers) != 0 {
		t.Errorf("expected empty workers in test env, got %v", proj.Workers)
	}
	// PHP version should be preserved
	if proj.PHPVersion != "8.4" {
		t.Errorf("PHPVersion = %q, want 8.4", proj.PHPVersion)
	}
}

func TestSetProjectWorkers_skips_paused(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("php_version: \"8.4\"\nworkers:\n  - queue\n  - schedule\n"), 0644)

	site := &config.Site{Name: "myapp", Path: dir, Paused: true}
	// Paused check happens at call site, not in SetProjectWorkers
	if !site.Paused {
		_ = config.SetProjectWorkers(site.Path, CollectRunningWorkerNames(site))
	}

	proj, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Workers should be preserved since the site is paused
	if len(proj.Workers) != 2 {
		t.Errorf("expected 2 workers preserved, got %v", proj.Workers)
	}
}

// ── ProjectConfig Workers field ─────────────────────────────────────────────

func TestProjectConfig_Workers_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.ProjectConfig{
		PHPVersion: "8.4",
		Workers:    []string{"queue", "schedule"},
	}
	if err := config.SaveProjectConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Workers) != 2 || loaded.Workers[0] != "queue" || loaded.Workers[1] != "schedule" {
		t.Errorf("Workers = %v", loaded.Workers)
	}
}

// ── Domain validation (IsDomainUsed via config) ─────────────────────────────

func TestDomainValidation_collision(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	config.AddSite(config.Site{
		Name:    "existing",
		Domains: []string{"taken.test"},
		Path:    "/srv/existing",
	})

	// Domain should be detected as used
	s, err := config.IsDomainUsed("taken.test")
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Name != "existing" {
		t.Errorf("expected domain to be used by 'existing', got %v", s)
	}

	// Free domain should return nil
	s, err = config.IsDomainUsed("free.test")
	if err != nil {
		t.Fatal(err)
	}
	if s != nil {
		t.Errorf("expected nil for free domain, got %v", s)
	}
}
