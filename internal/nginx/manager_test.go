package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// setupConfD points NginxConfD() at a temp dir via XDG_DATA_HOME and returns the conf.d path.
func setupConfD(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	return filepath.Join(tmp, "lerd", "nginx", "conf.d")
}

func readConf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// ── phpShort ──────────────────────────────────────────────────────────────────

func TestPhpShort(t *testing.T) {
	cases := []struct{ in, want string }{
		{"8.3", "83"},
		{"8.4", "84"},
		{"7.4", "74"},
		{"8.10", "810"},
	}
	for _, c := range cases {
		got := phpShort(c.in)
		if got != c.want {
			t.Errorf("phpShort(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── GetTemplate ───────────────────────────────────────────────────────────────

func TestGetTemplate_vhost(t *testing.T) {
	data, err := GetTemplate("vhost.conf.tmpl")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if !strings.Contains(string(data), "server_name") {
		t.Error("vhost template missing server_name directive")
	}
}

func TestGetTemplate_vhostSSL(t *testing.T) {
	data, err := GetTemplate("vhost-ssl.conf.tmpl")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if !strings.Contains(string(data), "ssl_certificate") {
		t.Error("SSL vhost template missing ssl_certificate directive")
	}
}

func TestGetTemplate_missing(t *testing.T) {
	_, err := GetTemplate("nonexistent.tmpl")
	if err == nil {
		t.Error("expected error for missing template")
	}
}

// ── GenerateVhost ─────────────────────────────────────────────────────────────

func TestGenerateVhost_createsConfFile(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/srv/myapp"}
	if err := GenerateVhost(site, "8.3"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "myapp.test.conf"))
	if !strings.Contains(content, "server_name myapp.test") {
		t.Errorf("expected server_name myapp.test in:\n%s", content)
	}
	if !strings.Contains(content, "root /srv/myapp/public") {
		t.Errorf("expected root path in:\n%s", content)
	}
	if !strings.Contains(content, "lerd-php83-fpm") {
		t.Errorf("expected PHP FPM reference in:\n%s", content)
	}
}

func TestGenerateVhost_phpVersionShort(t *testing.T) {
	setupConfD(t)
	site := config.Site{Name: "app", Domains: []string{"app.test"}, Path: "/srv/app"}
	if err := GenerateVhost(site, "8.4"); err != nil {
		t.Fatal(err)
	}
	// Verify phpShort is applied correctly in the template
	confD := filepath.Join(os.Getenv("XDG_DATA_HOME"), "lerd", "nginx", "conf.d")
	content := readConf(t, filepath.Join(confD, "app.test.conf"))
	if !strings.Contains(content, "lerd-php84-fpm") {
		t.Errorf("expected lerd-php84-fpm in:\n%s", content)
	}
	if strings.Contains(content, "lerd-php8.4-fpm") {
		t.Error("PHP version should not contain dots in FPM name")
	}
}

// ── GenerateSSLVhost ──────────────────────────────────────────────────────────

func TestGenerateSSLVhost_createsSSLConfFile(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/srv/myapp"}
	if err := GenerateSSLVhost(site, "8.3"); err != nil {
		t.Fatalf("GenerateSSLVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "myapp.test-ssl.conf"))
	if !strings.Contains(content, "listen 443 ssl") {
		t.Errorf("expected 443 ssl listen in:\n%s", content)
	}
	if !strings.Contains(content, "ssl_certificate") {
		t.Errorf("expected ssl_certificate in:\n%s", content)
	}
	// CertDomain defaults to site.PrimaryDomain() for own sites
	if !strings.Contains(content, "myapp.test.crt") {
		t.Errorf("expected cert file named after domain in:\n%s", content)
	}
	// HTTP→HTTPS redirect server block
	if !strings.Contains(content, "return 302 https://") {
		t.Errorf("expected HTTP redirect in:\n%s", content)
	}
}

// ── Multi-domain vhost ───────────────────────────────────────────────────────

func TestGenerateVhost_multiDomain(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "myapp", Domains: []string{"myapp.test", "api.test", "admin.test"}, Path: "/srv/myapp"}
	if err := GenerateVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "myapp.test.conf"))
	// server_name should list all domains plus wildcards
	if !strings.Contains(content, "server_name myapp.test *.myapp.test api.test *.api.test admin.test *.admin.test") {
		t.Errorf("expected all domains with wildcards in server_name, got:\n%s", content)
	}
}

func TestGenerateSSLVhost_multiDomain(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "myapp", Domains: []string{"myapp.test", "api.test"}, Path: "/srv/myapp"}
	if err := GenerateSSLVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateSSLVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "myapp.test-ssl.conf"))
	// Both server blocks should list all domains with wildcards
	if !strings.Contains(content, "server_name myapp.test *.myapp.test api.test *.api.test") {
		t.Errorf("expected all domains with wildcards in server_name, got:\n%s", content)
	}
	// Cert should be named after primary domain only
	if !strings.Contains(content, "myapp.test.crt") {
		t.Errorf("expected cert named after primary domain, got:\n%s", content)
	}
}

func TestGenerateVhost_confFileNamedAfterPrimary(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "myapp", Domains: []string{"primary.test", "alias.test"}, Path: "/srv/myapp"}
	if err := GenerateVhost(site, "8.3"); err != nil {
		t.Fatal(err)
	}
	// File should be named after primary domain
	if _, err := os.Stat(filepath.Join(confD, "primary.test.conf")); err != nil {
		t.Error("expected conf file named primary.test.conf")
	}
	// Should NOT create a file for the alias
	if _, err := os.Stat(filepath.Join(confD, "alias.test.conf")); !os.IsNotExist(err) {
		t.Error("should not create separate conf file for alias domain")
	}
}

// ── GenerateWorktreeVhost ─────────────────────────────────────────────────────

func TestGenerateWorktreeVhost_createsConfFile(t *testing.T) {
	confD := setupConfD(t)
	if err := GenerateWorktreeVhost("feat-x.myapp.test", "/srv/myapp-feat", "8.3"); err != nil {
		t.Fatalf("GenerateWorktreeVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "feat-x.myapp.test.conf"))
	if !strings.Contains(content, "server_name feat-x.myapp.test") {
		t.Errorf("expected worktree domain in:\n%s", content)
	}
	if !strings.Contains(content, "root /srv/myapp-feat/public") {
		t.Errorf("expected worktree path in:\n%s", content)
	}
}

// ── GenerateWorktreeSSLVhost ──────────────────────────────────────────────────

func TestGenerateWorktreeSSLVhost_usesParentCert(t *testing.T) {
	confD := setupConfD(t)
	if err := GenerateWorktreeSSLVhost("feat-x.myapp.test", "/srv/myapp-feat", "8.3", "myapp.test"); err != nil {
		t.Fatalf("GenerateWorktreeSSLVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "feat-x.myapp.test.conf"))
	if !strings.Contains(content, "server_name feat-x.myapp.test") {
		t.Errorf("expected worktree domain in:\n%s", content)
	}
	// Must use parent domain's cert (wildcard *.myapp.test), not feat-x.myapp.test
	if !strings.Contains(content, "myapp.test.crt") {
		t.Errorf("expected parent domain cert in:\n%s", content)
	}
	if strings.Contains(content, "feat-x.myapp.test.crt") {
		t.Error("worktree vhost must not reference its own cert file")
	}
}

// ── RemoveVhost ───────────────────────────────────────────────────────────────

func TestRemoveVhost_removesConfAndSSLConf(t *testing.T) {
	confD := setupConfD(t)
	os.MkdirAll(confD, 0755)
	os.WriteFile(filepath.Join(confD, "myapp.test.conf"), []byte("server {}"), 0644)
	os.WriteFile(filepath.Join(confD, "myapp.test-ssl.conf"), []byte("server {}"), 0644)

	if err := RemoveVhost("myapp.test"); err != nil {
		t.Fatalf("RemoveVhost: %v", err)
	}
	if _, err := os.Stat(filepath.Join(confD, "myapp.test.conf")); !os.IsNotExist(err) {
		t.Error("expected .conf to be removed")
	}
	if _, err := os.Stat(filepath.Join(confD, "myapp.test-ssl.conf")); !os.IsNotExist(err) {
		t.Error("expected -ssl.conf to be removed")
	}
}

func TestRemoveVhost_noError_whenMissing(t *testing.T) {
	setupConfD(t)
	// Should not error when files don't exist
	if err := RemoveVhost("ghost.test"); err != nil {
		t.Errorf("expected no error removing non-existent vhost, got: %v", err)
	}
}

// ── EnsureDefaultVhost ────────────────────────────────────────────────────────

func TestEnsureDefaultVhost_writesDefaultConf(t *testing.T) {
	confD := setupConfD(t)
	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("EnsureDefaultVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "_default.conf"))
	if !strings.Contains(content, "default_server") {
		t.Errorf("expected default_server in:\n%s", content)
	}
	if !strings.Contains(content, "404.html") {
		t.Errorf("expected 404.html error page reference in:\n%s", content)
	}
	if !strings.Contains(content, "ssl_reject_handshake on") {
		t.Errorf("expected ssl_reject_handshake in:\n%s", content)
	}
	// Verify error page HTML was written
	errorPage := filepath.Join(os.Getenv("XDG_DATA_HOME"), "lerd", "error-pages", "404.html")
	if _, err := os.Stat(errorPage); err != nil {
		t.Errorf("expected error page at %s", errorPage)
	}
}
