package serviceops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// reprovRecorder captures calls to the database/bucket creators. Tests swap
// the package-level seams (reprovDB, reprovBucket) before exercising
// ReprovisionLinkedSites.
type reprovRecorder struct {
	dbCalls     []reprovDBCall
	bucketCalls []string
	dbErrFor    map[string]error // keyed by db name
}
type reprovDBCall struct {
	Service string
	Name    string
}

func stubReprovProvision(t *testing.T) *reprovRecorder {
	t.Helper()
	rec := &reprovRecorder{dbErrFor: map[string]error{}}
	prevDB := reprovDB
	prevBucket := reprovBucket
	reprovDB = func(svc, name string) (bool, error) {
		rec.dbCalls = append(rec.dbCalls, reprovDBCall{Service: svc, Name: name})
		if err, ok := rec.dbErrFor[name]; ok {
			return false, err
		}
		return true, nil
	}
	reprovBucket = func(name string) (bool, error) {
		rec.bucketCalls = append(rec.bucketCalls, name)
		return true, nil
	}
	t.Cleanup(func() {
		reprovDB = prevDB
		reprovBucket = prevBucket
	})
	return rec
}

func mkSiteWithLerdYAML(t *testing.T, name string, services ...string) {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("services:\n")
	for _, s := range services {
		b.WriteString("  - " + s + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write lerd.yaml: %v", err)
	}
	if err := config.AddSite(config.Site{Name: name, Domains: []string{name + ".test"}, Path: dir}); err != nil {
		t.Fatalf("AddSite %s: %v", name, err)
	}
}

func mkSiteWithEnv(t *testing.T, name, envContent string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if err := config.AddSite(config.Site{Name: name, Domains: []string{name + ".test"}, Path: dir}); err != nil {
		t.Fatalf("AddSite %s: %v", name, err)
	}
	return dir
}

func TestReprovisionLinkedSites_MysqlFamily_CreatesDBPerSite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := stubReprovProvision(t)

	mkSiteWithLerdYAML(t, "site-a", "mariadb")
	mkSiteWithLerdYAML(t, "site-b", "mariadb")

	if err := ReprovisionLinkedSites("mariadb", nil); err != nil {
		t.Fatalf("ReprovisionLinkedSites: %v", err)
	}

	if len(rec.dbCalls) != 2 {
		t.Fatalf("expected 2 CreateDatabase calls, got %d (%v)", len(rec.dbCalls), rec.dbCalls)
	}
	got := map[string]bool{}
	for _, c := range rec.dbCalls {
		if c.Service != "mariadb" {
			t.Errorf("call service = %q, want mariadb", c.Service)
		}
		got[c.Name] = true
	}
	if !got["site_a"] || !got["site_b"] {
		t.Errorf("expected dbs site_a and site_b, got %v", got)
	}
	if len(rec.bucketCalls) != 0 {
		t.Errorf("bucket creator called for db family: %v", rec.bucketCalls)
	}
}

func TestReprovisionLinkedSites_PostgresFamily_CreatesDBPerSite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := stubReprovProvision(t)

	mkSiteWithLerdYAML(t, "blog", "postgres")

	if err := ReprovisionLinkedSites("postgres", nil); err != nil {
		t.Fatalf("ReprovisionLinkedSites: %v", err)
	}
	if len(rec.dbCalls) != 1 || rec.dbCalls[0].Name != "blog" {
		t.Errorf("expected one db call for 'blog', got %v", rec.dbCalls)
	}
}

func TestReprovisionLinkedSites_ObjectStorage_CreatesBucketPerSite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := stubReprovProvision(t)

	mkSiteWithEnv(t, "uploads-app", "AWS_BUCKET=uploads-app\nFILESYSTEM_DISK=s3\nlerd-rustfs\n")

	if err := ReprovisionLinkedSites("rustfs", nil); err != nil {
		t.Fatalf("ReprovisionLinkedSites: %v", err)
	}
	if len(rec.bucketCalls) != 1 || rec.bucketCalls[0] != "uploads-app" {
		t.Errorf("expected bucket call for uploads-app, got %v", rec.bucketCalls)
	}
}

func TestReprovisionLinkedSites_RedisFamily_NoOps(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := stubReprovProvision(t)

	mkSiteWithEnv(t, "cache-app", "REDIS_HOST=lerd-redis\n")

	if err := ReprovisionLinkedSites("redis", nil); err != nil {
		t.Fatalf("ReprovisionLinkedSites: %v", err)
	}
	if len(rec.dbCalls) != 0 || len(rec.bucketCalls) != 0 {
		t.Errorf("redis should be a no-op, got dbs=%v buckets=%v", rec.dbCalls, rec.bucketCalls)
	}
}

func TestReprovisionLinkedSites_SkipsIgnoredAndPaused(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := stubReprovProvision(t)

	yamlActive := t.TempDir()
	os.WriteFile(filepath.Join(yamlActive, ".lerd.yaml"), []byte("services:\n  - mariadb\n"), 0o644)
	if err := config.AddSite(config.Site{Name: "active", Domains: []string{"a.test"}, Path: yamlActive}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}
	yamlPaused := t.TempDir()
	os.WriteFile(filepath.Join(yamlPaused, ".lerd.yaml"), []byte("services:\n  - mariadb\n"), 0o644)
	if err := config.AddSite(config.Site{Name: "paused", Domains: []string{"p.test"}, Path: yamlPaused, Paused: true}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	if err := ReprovisionLinkedSites("mariadb", nil); err != nil {
		t.Fatalf("ReprovisionLinkedSites: %v", err)
	}
	if len(rec.dbCalls) != 1 || rec.dbCalls[0].Name != "active" {
		t.Errorf("expected only 'active' provisioned, got %v", rec.dbCalls)
	}
}

func TestReprovisionLinkedSites_PerSiteFailure_ContinuesAndJoinsErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := stubReprovProvision(t)
	rec.dbErrFor["site_b"] = errors.New("boom")

	mkSiteWithLerdYAML(t, "site-a", "mariadb")
	mkSiteWithLerdYAML(t, "site-b", "mariadb")
	mkSiteWithLerdYAML(t, "site-c", "mariadb")

	err := ReprovisionLinkedSites("mariadb", nil)
	if err == nil {
		t.Fatal("expected joined error for site-b failure")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should include underlying message, got %v", err)
	}
	if len(rec.dbCalls) != 3 {
		t.Errorf("expected all 3 sites attempted (continue-on-error), got %v", rec.dbCalls)
	}
}

func TestReprovisionLinkedSites_DBNameOverrideFromProjectConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := stubReprovProvision(t)

	dir := t.TempDir()
	yaml := "services:\n  - postgres\ndb:\n  database: custom_name\n"
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte(yaml), 0o644)
	if err := config.AddSite(config.Site{Name: "weird-site", Domains: []string{"w.test"}, Path: dir}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	if err := ReprovisionLinkedSites("postgres", nil); err != nil {
		t.Fatalf("ReprovisionLinkedSites: %v", err)
	}
	if len(rec.dbCalls) != 1 || rec.dbCalls[0].Name != "custom_name" {
		t.Errorf("expected db name 'custom_name' from .lerd.yaml override, got %v", rec.dbCalls)
	}
}

func TestReprovisionLinkedSites_DBNameFallbackToEnvFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := stubReprovProvision(t)

	mkSiteWithEnv(t, "envapp", "DB_HOST=lerd-mysql\nDB_DATABASE=env_chosen\n")

	if err := ReprovisionLinkedSites("mysql", nil); err != nil {
		t.Fatalf("ReprovisionLinkedSites: %v", err)
	}
	if len(rec.dbCalls) != 1 || rec.dbCalls[0].Name != "env_chosen" {
		t.Errorf("expected db name 'env_chosen' from .env, got %v", rec.dbCalls)
	}
}

func TestReprovisionLinkedSites_DBNameFallbackToSiteName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	rec := stubReprovProvision(t)

	// .lerd.yaml lists postgres but no db.database; .env is absent.
	mkSiteWithLerdYAML(t, "no-overrides", "postgres")

	if err := ReprovisionLinkedSites("postgres", nil); err != nil {
		t.Fatalf("ReprovisionLinkedSites: %v", err)
	}
	if len(rec.dbCalls) != 1 || rec.dbCalls[0].Name != "no_overrides" {
		t.Errorf("expected derived db 'no_overrides', got %v", rec.dbCalls)
	}
}

func TestReprovisionLinkedSites_EmitsPerSiteEvent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	stubReprovProvision(t)

	mkSiteWithLerdYAML(t, "site-x", "mariadb")

	var events []PhaseEvent
	if err := ReprovisionLinkedSites("mariadb", func(e PhaseEvent) { events = append(events, e) }); err != nil {
		t.Fatalf("ReprovisionLinkedSites: %v", err)
	}

	var sawSiteEvent, sawSummary bool
	for _, e := range events {
		if e.Phase == "reprovisioning_site" && strings.Contains(e.Message, "site-x") {
			sawSiteEvent = true
		}
		if e.Phase == "reprovisioning_sites" {
			sawSummary = true
		}
	}
	if !sawSummary {
		t.Errorf("expected reprovisioning_sites phase, got %v", events)
	}
	if !sawSiteEvent {
		t.Errorf("expected reprovisioning_site phase mentioning site-x, got %v", events)
	}
}
