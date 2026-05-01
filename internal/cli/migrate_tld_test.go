package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
)

func TestSitesWithTLD_PicksOnlyMatchingSuffix(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	for _, s := range []config.Site{
		{Name: "alpha", Path: filepath.Join(tmp, "alpha"), Domains: []string{"alpha.test"}},
		{Name: "beta", Path: filepath.Join(tmp, "beta"), Domains: []string{"beta.localhost"}},
		{Name: "gamma", Path: filepath.Join(tmp, "gamma"), Domains: []string{"gamma.test", "gamma-alt.test"}},
		{Name: "delta", Path: filepath.Join(tmp, "delta"), Domains: []string{"delta.example.com"}},
	} {
		if err := config.AddSite(s); err != nil {
			t.Fatalf("AddSite %s: %v", s.Name, err)
		}
	}

	got := sitesWithTLD("test")
	want := []string{"alpha", "gamma"}
	if !slices.Equal(got, want) {
		t.Errorf("sitesWithTLD(test) = %v, want %v", got, want)
	}
}

func TestMigrateSiteTLD_RewritesDomainsAndEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		t.Fatalf("mkdir NginxConfD: %v", err)
	}

	siteDir := filepath.Join(tmp, "alpha")
	if err := os.MkdirAll(siteDir, 0755); err != nil {
		t.Fatalf("mkdir site: %v", err)
	}
	envPath := filepath.Join(siteDir, ".env")
	if err := os.WriteFile(envPath, []byte("APP_URL=http://alpha.test\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	staleVhost := filepath.Join(config.NginxConfD(), "alpha.test.conf")
	if err := os.WriteFile(staleVhost, []byte("server {}\n"), 0644); err != nil {
		t.Fatalf("write vhost: %v", err)
	}

	certsDir := filepath.Join(config.CertsDir(), "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatalf("mkdir certs: %v", err)
	}
	staleCrt := filepath.Join(certsDir, "alpha.test.crt")
	staleKey := filepath.Join(certsDir, "alpha.test.key")
	for _, p := range []string{staleCrt, staleKey} {
		if err := os.WriteFile(p, []byte("dummy\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	if err := config.AddSite(config.Site{
		Name:    "alpha",
		Path:    siteDir,
		Domains: []string{"alpha.test"},
		Secured: true,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	changed := migrateSiteTLD("test", "localhost", true)
	if !slices.Equal(changed, []string{"alpha"}) {
		t.Fatalf("changed = %v, want [alpha]", changed)
	}

	site, err := config.FindSite("alpha")
	if err != nil {
		t.Fatalf("FindSite: %v", err)
	}
	if got := site.PrimaryDomain(); got != "alpha.localhost" {
		t.Errorf("primary domain = %q, want alpha.localhost", got)
	}
	if site.Secured {
		t.Errorf("Secured should be false after forceUnsecure")
	}

	envBytes, _ := os.ReadFile(envPath)
	if want := "APP_URL=http://alpha.localhost"; string(envBytes) == "" || !contains(envBytes, want) {
		t.Errorf(".env not updated; got %q, want substring %q", envBytes, want)
	}

	if _, err := os.Stat(staleVhost); !os.IsNotExist(err) {
		t.Errorf("stale vhost should have been removed; stat err = %v", err)
	}
	if _, err := os.Stat(staleCrt); !os.IsNotExist(err) {
		t.Errorf("stale .crt should have been removed; stat err = %v", err)
	}
	if _, err := os.Stat(staleKey); !os.IsNotExist(err) {
		t.Errorf("stale .key should have been removed; stat err = %v", err)
	}
}

func TestMigrateWorktreeVhosts_RewritesConfsAndEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		t.Fatalf("mkdir confD: %v", err)
	}
	wtPath := filepath.Join(tmp, "alpha-wt")
	if err := os.MkdirAll(wtPath, 0755); err != nil {
		t.Fatalf("mkdir wt: %v", err)
	}
	envPath := filepath.Join(wtPath, ".env")
	if err := os.WriteFile(envPath, []byte("APP_URL=http://feat-x.alpha.test\n"), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	staleConf := filepath.Join(config.NginxConfD(), "feat-x.alpha.test.conf")
	if err := os.WriteFile(staleConf, []byte("server {}\n"), 0644); err != nil {
		t.Fatalf("write stale conf: %v", err)
	}

	worktrees := []gitpkg.Worktree{
		{Name: "wt", Branch: "feat-x", Path: wtPath, Domain: "feat-x.alpha.test"},
	}
	migrateWorktreeVhosts(worktrees, "alpha.localhost", "8.4", false)

	if _, err := os.Stat(staleConf); !os.IsNotExist(err) {
		t.Errorf("stale worktree conf should be gone; stat err = %v", err)
	}
	freshConf := filepath.Join(config.NginxConfD(), "feat-x.alpha.localhost.conf")
	if _, err := os.Stat(freshConf); err != nil {
		t.Errorf("fresh worktree conf missing: %v", err)
	}

	envBytes, _ := os.ReadFile(envPath)
	if !contains(envBytes, "APP_URL=http://feat-x.alpha.localhost") {
		t.Errorf(".env not updated; got %q", envBytes)
	}
}

func TestMigrateSiteTLD_NoOpWhenSameTLD(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if err := config.AddSite(config.Site{
		Name: "x", Path: filepath.Join(tmp, "x"), Domains: []string{"x.test"},
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	if got := migrateSiteTLD("test", "test", false); got != nil {
		t.Errorf("noop expected, got %v", got)
	}
}

func contains(haystack []byte, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}
