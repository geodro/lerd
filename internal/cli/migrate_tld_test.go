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
	migrateWorktreeVhosts(worktrees, "alpha.localhost", "8.4", "alpha", false)

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

// TestMigrateSiteTLD_ReissuesCertForSecuredSiteWithWorktree pins the fix
// for the regression where migrating a secured site's TLD regenerated the
// worktree vhosts but never reissued the parent cert. SSL handshakes to
// branch.<newPrimary> would fail because the cert SANs still carried the
// old TLD's wildcard. The migration must produce a cert at the NEW primary
// path that covers the renamed worktree subdomain.
func TestMigrateSiteTLD_ReissuesCertForSecuredSiteWithWorktree(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Fake mkcert that writes its SAN list into the cert/key files so the
	// test can assert the new TLD's wildcard SAN was passed.
	fakeMkcert := `#!/bin/sh
CRT=""
KEY=""
SANS=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -cert-file) shift; CRT="$1" ;;
    -key-file) shift; KEY="$1" ;;
    *) SANS="$SANS $1" ;;
  esac
  shift
done
echo "$SANS" > "$CRT"
echo "FAKE-KEY" > "$KEY"
`
	if err := os.WriteFile(filepath.Join(binDir, "mkcert"), []byte(fakeMkcert), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(config.NginxConfD(), 0755); err != nil {
		t.Fatal(err)
	}

	// Simulate a worktree via .git/worktrees/<entry>/ — DetectWorktrees
	// reads gitdir + HEAD from the entry dir.
	siteDir := filepath.Join(tmp, "alpha")
	if err := os.MkdirAll(siteDir, 0755); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(tmp, "feat-x-checkout")
	if err := os.MkdirAll(checkout, 0755); err != nil {
		t.Fatal(err)
	}
	wtMeta := filepath.Join(siteDir, ".git", "worktrees", "feat-x")
	os.MkdirAll(wtMeta, 0755)
	os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/feat-x\n"), 0644)
	os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0644)

	if err := os.WriteFile(filepath.Join(siteDir, ".env"), []byte("APP_URL=https://alpha.test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".env"), []byte("APP_URL=https://feat-x.alpha.test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := config.AddSite(config.Site{
		Name:       "alpha",
		Path:       siteDir,
		Domains:    []string{"alpha.test"},
		PHPVersion: "8.4",
		Secured:    true,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	// Seed an OLD cert at the old primary so we can verify it's torn down.
	certsDir := filepath.Join(tmp, "lerd", "certs", "sites")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certsDir, "alpha.test.crt"), []byte("OLD-CERT"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(certsDir, "alpha.test.key"), []byte("OLD-KEY"), 0644); err != nil {
		t.Fatal(err)
	}

	changed := migrateSiteTLD("test", "localhost", false)
	if !slices.Equal(changed, []string{"alpha"}) {
		t.Fatalf("changed = %v, want [alpha]", changed)
	}

	// New cert must exist at the new primary.
	newCert := filepath.Join(certsDir, "alpha.localhost.crt")
	body, err := os.ReadFile(newCert)
	if err != nil {
		t.Fatalf("expected cert at %s, got %v", newCert, err)
	}
	// Cert SANs must include the renamed worktree wildcard.
	wantSAN := "*.feat-x.alpha.localhost"
	if !contains(body, wantSAN) {
		t.Errorf("cert %s missing SAN %q; got %q", newCert, wantSAN, body)
	}
	// Old cert files must be cleaned up so the certs/sites dir doesn't
	// accumulate stale entries across migrations.
	if _, err := os.Stat(filepath.Join(certsDir, "alpha.test.crt")); !os.IsNotExist(err) {
		t.Errorf("old cert should be removed after migration; stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(certsDir, "alpha.test.key")); !os.IsNotExist(err) {
		t.Errorf("old key should be removed after migration; stat err = %v", err)
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
