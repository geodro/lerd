package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSyncProjectDomains_keepsNonDefaultTLDWhole(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{})

	// Default TLD "test" is stripped to a bare label; ".local" is kept whole so
	// a relink restores alice.local rather than re-suffixing to alice.local.test.
	if err := SyncProjectDomains(dir, []string{"alice.test", "alice.local"}, "test"); err != nil {
		t.Fatalf("SyncProjectDomains: %v", err)
	}

	got := loadConfig(t, dir).Domains
	want := map[string]bool{"alice": true, "alice.local": true}
	if len(got) != len(want) {
		t.Fatalf("Domains = %v, want keys %v", got, want)
	}
	for _, d := range got {
		if !want[d] {
			t.Errorf("unexpected .lerd.yaml domain %q (TLD not preserved correctly)", d)
		}
	}
}

// helper: create a .lerd.yaml with the given config in a temp dir.
func setupProjectConfig(t *testing.T, cfg *ProjectConfig) string {
	t.Helper()
	dir := t.TempDir()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func loadConfig(t *testing.T, dir string) *ProjectConfig {
	t.Helper()
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// ── updateProjectConfig (no-op when file missing) ───────────────────────────

func TestUpdateProjectConfig_NoOpWhenMissing(t *testing.T) {
	dir := t.TempDir()
	called := false
	err := updateProjectConfig(dir, func(cfg *ProjectConfig) {
		called = true
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("callback should not be called when .lerd.yaml is missing")
	}
}

func TestUpdateProjectConfig_CallsWhenExists(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{PHPVersion: "8.1"})
	called := false
	err := updateProjectConfig(dir, func(cfg *ProjectConfig) {
		called = true
		cfg.PHPVersion = "8.4"
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("callback should be called")
	}
	cfg := loadConfig(t, dir)
	if cfg.PHPVersion != "8.4" {
		t.Errorf("PHPVersion = %q, want 8.4", cfg.PHPVersion)
	}
}

// ── SetProjectSecured ───────────────────────────────────────────────────────

func TestSetProjectSecured(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Secured: false})
	if err := SetProjectSecured(dir, true); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if !cfg.Secured {
		t.Error("expected Secured=true")
	}
}

func TestSetProjectSecured_NoOpWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if err := SetProjectSecured(dir, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".lerd.yaml")); !os.IsNotExist(err) {
		t.Error(".lerd.yaml should not be created")
	}
}

// ── SetProjectPHPVersion ────────────────────────────────────────────────────

func TestSetProjectPHPVersion(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{PHPVersion: "8.1"})
	if err := SetProjectPHPVersion(dir, "8.4"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if cfg.PHPVersion != "8.4" {
		t.Errorf("PHPVersion = %q, want 8.4", cfg.PHPVersion)
	}
}

func TestSetProjectPHPVersion_PreservesOtherFields(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{
		PHPVersion: "8.1",
		Framework:  "laravel",
		Domains:    []string{"myapp"},
		Secured:    true,
	})
	if err := SetProjectPHPVersion(dir, "8.4"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if cfg.PHPVersion != "8.4" {
		t.Errorf("PHPVersion = %q, want 8.4", cfg.PHPVersion)
	}
	if cfg.Framework != "laravel" {
		t.Errorf("Framework = %q, want laravel", cfg.Framework)
	}
	if len(cfg.Domains) != 1 || cfg.Domains[0] != "myapp" {
		t.Errorf("Domains = %v, want [myapp]", cfg.Domains)
	}
	if !cfg.Secured {
		t.Error("expected Secured=true")
	}
}

func TestSetProjectPHPVersion_NoOpWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if err := SetProjectPHPVersion(dir, "8.4"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".lerd.yaml")); !os.IsNotExist(err) {
		t.Error(".lerd.yaml should not be created")
	}
}

// ── SetProjectWorkers ───────────────────────────────────────────────────────

func TestSetProjectWorkers(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Workers: []string{"queue"}})
	if err := SetProjectWorkers(dir, []string{"queue", "schedule"}); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if len(cfg.Workers) != 2 || cfg.Workers[0] != "queue" || cfg.Workers[1] != "schedule" {
		t.Errorf("Workers = %v, want [queue schedule]", cfg.Workers)
	}
}

func TestSetProjectWorkers_NoOpWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if err := SetProjectWorkers(dir, []string{"queue"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".lerd.yaml")); !os.IsNotExist(err) {
		t.Error(".lerd.yaml should not be created")
	}
}

// ── SetProjectDomains ───────────────────────────────────────────────────────

func TestSetProjectDomains(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Domains: []string{"old"}})
	if err := SetProjectDomains(dir, []string{"new1", "new2"}); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if len(cfg.Domains) != 2 || cfg.Domains[0] != "new1" || cfg.Domains[1] != "new2" {
		t.Errorf("Domains = %v, want [new1 new2]", cfg.Domains)
	}
}

// ── SyncProjectDomains ──────────────────────────────────────────────────────

func TestSyncProjectDomains_MergesAndDeduplicates(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Domains: []string{"myapp", "conflict-domain"}})
	err := SyncProjectDomains(dir, []string{"myapp.test", "api.test"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	// myapp and api from fullDomains, conflict-domain preserved from existing
	want := []string{"myapp", "api", "conflict-domain"}
	if len(cfg.Domains) != len(want) {
		t.Fatalf("Domains = %v, want %v", cfg.Domains, want)
	}
	for i, d := range want {
		if cfg.Domains[i] != d {
			t.Errorf("Domains[%d] = %q, want %q", i, cfg.Domains[i], d)
		}
	}
}

func TestSyncProjectDomains_CaseInsensitiveDedup(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Domains: []string{"MyApp"}})
	err := SyncProjectDomains(dir, []string{"myapp.test"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	// "myapp" from fullDomains wins, "MyApp" is deduplicated
	if len(cfg.Domains) != 1 || cfg.Domains[0] != "myapp" {
		t.Errorf("Domains = %v, want [myapp]", cfg.Domains)
	}
}

func TestSyncProjectDomains_NoOpWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if err := SyncProjectDomains(dir, []string{"myapp.test"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".lerd.yaml")); !os.IsNotExist(err) {
		t.Error(".lerd.yaml should not be created")
	}
}

// ── RemoveProjectDomain ─────────────────────────────────────────────────────

func TestRemoveProjectDomain(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Domains: []string{"myapp", "api", "admin"}})
	if err := RemoveProjectDomain(dir, "api"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if len(cfg.Domains) != 2 || cfg.Domains[0] != "myapp" || cfg.Domains[1] != "admin" {
		t.Errorf("Domains = %v, want [myapp admin]", cfg.Domains)
	}
}

func TestRemoveProjectDomain_CaseInsensitive(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Domains: []string{"MyApp", "api"}})
	if err := RemoveProjectDomain(dir, "myapp"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if len(cfg.Domains) != 1 || cfg.Domains[0] != "api" {
		t.Errorf("Domains = %v, want [api]", cfg.Domains)
	}
}

func TestRemoveProjectDomain_NotFound(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Domains: []string{"myapp"}})
	if err := RemoveProjectDomain(dir, "other"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if len(cfg.Domains) != 1 || cfg.Domains[0] != "myapp" {
		t.Errorf("Domains = %v, want [myapp]", cfg.Domains)
	}
}

// ── SetProjectFrameworkVersion ──────────────────────────────────────────────

func TestSetProjectFrameworkVersion(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Framework: "laravel", FrameworkVersion: "11"})
	if err := SetProjectFrameworkVersion(dir, "12"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if cfg.FrameworkVersion != "12" {
		t.Errorf("FrameworkVersion = %q, want 12", cfg.FrameworkVersion)
	}
}

// ── SetProjectFrameworkDef ──────────────────────────────────────────────────

func TestSetProjectFrameworkDef(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Framework: "custom"})
	def := &Framework{Name: "custom", Label: "Custom Framework"}
	if err := SetProjectFrameworkDef(dir, def); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if cfg.FrameworkDef == nil || cfg.FrameworkDef.Label != "Custom Framework" {
		t.Errorf("FrameworkDef = %v, want Label=Custom Framework", cfg.FrameworkDef)
	}
}

// ── SetProjectCustomWorker ──────────────────────────────────────────────────

func TestSetProjectCustomWorker_New(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{})
	w := FrameworkWorker{Label: "My Worker", Command: "php worker.php"}
	if err := SetProjectCustomWorker(dir, "myworker", w); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if cfg.CustomWorkers == nil {
		t.Fatal("CustomWorkers is nil")
	}
	got, exists := cfg.CustomWorkers["myworker"]
	if !exists {
		t.Fatal("myworker not found")
	}
	if got.Label != "My Worker" || got.Command != "php worker.php" {
		t.Errorf("got %+v", got)
	}
}

func TestSetProjectCustomWorker_Update(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{
		CustomWorkers: map[string]FrameworkWorker{
			"myworker": {Label: "Old", Command: "old"},
		},
	})
	w := FrameworkWorker{Label: "New", Command: "new"}
	if err := SetProjectCustomWorker(dir, "myworker", w); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if cfg.CustomWorkers["myworker"].Label != "New" {
		t.Errorf("expected updated label, got %q", cfg.CustomWorkers["myworker"].Label)
	}
}

// ── RemoveProjectCustomWorker ───────────────────────────────────────────────

func TestRemoveProjectCustomWorker(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{
		CustomWorkers: map[string]FrameworkWorker{
			"a": {Label: "A"},
			"b": {Label: "B"},
		},
	})
	if err := RemoveProjectCustomWorker(dir, "a"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if _, exists := cfg.CustomWorkers["a"]; exists {
		t.Error("worker a should be removed")
	}
	if _, exists := cfg.CustomWorkers["b"]; !exists {
		t.Error("worker b should still exist")
	}
}

func TestRemoveProjectCustomWorker_LastCleansMap(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{
		CustomWorkers: map[string]FrameworkWorker{
			"only": {Label: "Only"},
		},
	})
	if err := RemoveProjectCustomWorker(dir, "only"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if cfg.CustomWorkers != nil {
		t.Errorf("CustomWorkers should be nil, got %v", cfg.CustomWorkers)
	}
}

func TestRemoveProjectCustomWorker_NotFound(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{
		CustomWorkers: map[string]FrameworkWorker{
			"a": {Label: "A"},
		},
	})
	err := RemoveProjectCustomWorker(dir, "missing")
	if err == nil {
		t.Fatal("expected error for missing worker")
	}
	if _, ok := err.(*WorkerNotFoundError); !ok {
		t.Errorf("expected WorkerNotFoundError, got %T", err)
	}
}

func TestRemoveProjectCustomWorker_NoOpWhenMissing(t *testing.T) {
	dir := t.TempDir()
	err := RemoveProjectCustomWorker(dir, "anything")
	if err != nil {
		t.Fatal(err)
	}
}

// ── ReplaceProjectDBService ─────────────────────────────────────────────────

func TestReplaceProjectDBService_ReplacesExisting(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{
		Services: []ProjectService{
			{Name: "mysql"},
			{Name: "redis"},
		},
	})
	if err := ReplaceProjectDBService(dir, "postgres"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if len(cfg.Services) != 2 {
		t.Fatalf("expected 2 services, got %d: %v", len(cfg.Services), cfg.Services)
	}
	if cfg.Services[0].Name != "redis" {
		t.Errorf("Services[0] = %q, want redis", cfg.Services[0].Name)
	}
	if cfg.Services[1].Name != "postgres" {
		t.Errorf("Services[1] = %q, want postgres", cfg.Services[1].Name)
	}
}

func TestReplaceProjectDBService_AddsWhenNoDB(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{
		Services: []ProjectService{{Name: "redis"}},
	})
	if err := ReplaceProjectDBService(dir, "sqlite"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if len(cfg.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(cfg.Services))
	}
	if cfg.Services[1].Name != "sqlite" {
		t.Errorf("Services[1] = %q, want sqlite", cfg.Services[1].Name)
	}
}

func TestReplaceProjectDBService_CreatesFileIfMissing(t *testing.T) {
	dir := t.TempDir()
	if err := ReplaceProjectDBService(dir, "mysql"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	if len(cfg.Services) != 1 || cfg.Services[0].Name != "mysql" {
		t.Errorf("Services = %v, want [{mysql}]", cfg.Services)
	}
}

func TestReplaceProjectDBService_ReplacesFamilyAlternate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "lerd", "services"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `name: postgres-pgvector
family: postgres
image: docker.io/pgvector/pgvector:pg18
`
	if err := os.WriteFile(filepath.Join(tmp, "lerd", "services", "postgres-pgvector.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write fake service: %v", err)
	}
	dir := setupProjectConfig(t, &ProjectConfig{
		Services: []ProjectService{
			{Name: "postgres-pgvector"},
			{Name: "redis"},
		},
	})
	if err := ReplaceProjectDBService(dir, "mysql"); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, dir)
	for _, svc := range cfg.Services {
		if svc.Name == "postgres-pgvector" {
			t.Errorf("postgres-pgvector should have been replaced, got services: %v", cfg.Services)
		}
	}
}

func TestIsDBServiceName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "lerd", "services"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yaml := `name: postgres-pgvector
family: postgres
image: docker.io/pgvector/pgvector:pg18
`
	if err := os.WriteFile(filepath.Join(tmp, "lerd", "services", "postgres-pgvector.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write fake service: %v", err)
	}
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"sqlite", true},
		{"mysql", true},
		{"postgres", true},
		{"postgres-pgvector", true},
		{"redis", false},
		{"meilisearch", false},
		{"", false},
		{"made-up-service", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsDBServiceName(tc.name); got != tc.want {
				t.Errorf("IsDBServiceName(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// ── SetProjectWorkerReload ──────────────────────────────────────────────────

func TestSetProjectWorkerReload_EnableCreatesMissingFile(t *testing.T) {
	dir := t.TempDir() // no .lerd.yaml on disk

	if err := SetProjectWorkerReload(dir, "horizon", true); err != nil {
		t.Fatalf("SetProjectWorkerReload: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".lerd.yaml")); err != nil {
		t.Fatalf("expected .lerd.yaml to be created, stat: %v", err)
	}
	if !ProjectReloadsWorker(dir, "horizon") {
		t.Errorf("horizon should be opted into reload after enabling")
	}
}

func TestSetProjectWorkerReload_DisableOnMissingFileIsNoOp(t *testing.T) {
	dir := t.TempDir() // no .lerd.yaml on disk

	if err := SetProjectWorkerReload(dir, "horizon", false); err != nil {
		t.Fatalf("SetProjectWorkerReload: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".lerd.yaml")); !os.IsNotExist(err) {
		t.Errorf("disabling on a project with no .lerd.yaml must not create one (stat err: %v)", err)
	}
}

func TestSetProjectWorkerReload_Toggle(t *testing.T) {
	dir := setupProjectConfig(t, &ProjectConfig{Domains: []string{"app"}})

	if err := SetProjectWorkerReload(dir, "horizon", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !loadConfig(t, dir).ReloadsWorker("horizon") {
		t.Fatalf("horizon should be enabled")
	}

	// Enabling again is idempotent (no duplicate entries).
	if err := SetProjectWorkerReload(dir, "horizon", true); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if got := loadConfig(t, dir).ReloadWorkers; len(got) != 1 {
		t.Fatalf("expected one entry, got %v", got)
	}

	if err := SetProjectWorkerReload(dir, "horizon", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	cfg := loadConfig(t, dir)
	if cfg.ReloadsWorker("horizon") {
		t.Errorf("horizon should be disabled")
	}
	if len(cfg.Domains) != 1 || cfg.Domains[0] != "app" {
		t.Errorf("unrelated fields should survive the toggle, got domains %v", cfg.Domains)
	}
}
