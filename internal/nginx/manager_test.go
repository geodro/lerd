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

// ── RepairVhosts ─────────────────────────────────────────────────────────────

// setupRepairEnv creates a temp dir with XDG env vars, conf.d and certs dirs,
// and writes sites.yaml. Returns confD and certsDir paths.
func setupRepairEnv(t *testing.T, sitesYAML string) (confD, certsDir string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))

	confD = filepath.Join(tmp, "lerd", "nginx", "conf.d")
	certsDir = filepath.Join(tmp, "lerd", "certs", "sites")
	os.MkdirAll(confD, 0755)
	os.MkdirAll(certsDir, 0755)

	sitesDir := filepath.Join(tmp, "lerd")
	os.MkdirAll(sitesDir, 0755)
	os.WriteFile(filepath.Join(sitesDir, "sites.yaml"), []byte(sitesYAML), 0644)
	return confD, certsDir
}

func TestRepairVhosts_missingCertSwitchesToHTTP(t *testing.T) {
	confD, _ := setupRepairEnv(t, `sites:
- name: myapp
  domains:
    - myapp.test
  path: /srv/myapp
  php_version: "8.4"
  secured: true
`)

	// Write an SSL vhost config that references a missing cert.
	sslConf := `server {
    listen 80;
    server_name myapp.test *.myapp.test;
    return 302 https://$host$request_uri;
}
server {
    listen 443 ssl;
    server_name myapp.test *.myapp.test;
    root /srv/myapp/public;
    ssl_certificate /etc/nginx/certs/myapp.test.crt;
    ssl_certificate_key /etc/nginx/certs/myapp.test.key;
}
`
	os.WriteFile(filepath.Join(confD, "myapp.test.conf"), []byte(sslConf), 0644)

	repairs := RepairVhosts()

	if len(repairs) != 1 || repairs[0].Domain != "myapp.test" || repairs[0].Reason != "missing-cert" {
		t.Fatalf("expected [{myapp.test missing-cert}], got %v", repairs)
	}

	// Verify the vhost was regenerated as HTTP.
	content := readConf(t, filepath.Join(confD, "myapp.test.conf"))
	if strings.Contains(content, "ssl_certificate") {
		t.Error("expected SSL directives to be removed after repair")
	}
	if !strings.Contains(content, "server_name myapp.test") {
		t.Error("expected server_name to be preserved")
	}

	// Verify site registry was updated.
	reg, err := config.LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range reg.Sites {
		if s.Name == "myapp" && s.Secured {
			t.Error("expected site.Secured to be false after repair")
		}
	}
}

func TestRepairVhosts_noOpWhenCertExists(t *testing.T) {
	confD, certsDir := setupRepairEnv(t, `sites:
- name: good
  domains:
    - good.test
  path: /srv/good
  php_version: "8.4"
  secured: true
`)

	sslConf := `server {
    listen 443 ssl;
    server_name good.test *.good.test;
    ssl_certificate /etc/nginx/certs/good.test.crt;
    ssl_certificate_key /etc/nginx/certs/good.test.key;
}
`
	os.WriteFile(filepath.Join(confD, "good.test.conf"), []byte(sslConf), 0644)
	os.WriteFile(filepath.Join(certsDir, "good.test.crt"), []byte("cert"), 0644)
	os.WriteFile(filepath.Join(certsDir, "good.test.key"), []byte("key"), 0644)

	repairs := RepairVhosts()

	if len(repairs) != 0 {
		t.Fatalf("expected no repairs, got %v", repairs)
	}

	content := readConf(t, filepath.Join(confD, "good.test.conf"))
	if !strings.Contains(content, "ssl_certificate") {
		t.Error("SSL vhost should not be modified when cert exists")
	}
}

func TestRepairVhosts_removesOrphanSSLVhost(t *testing.T) {
	confD, _ := setupRepairEnv(t, "sites: []\n")

	sslConf := `server {
    listen 443 ssl;
    server_name orphan.test;
    ssl_certificate /etc/nginx/certs/orphan.test.crt;
}
`
	os.WriteFile(filepath.Join(confD, "orphan.test.conf"), []byte(sslConf), 0644)

	repairs := RepairVhosts()

	if len(repairs) != 1 || repairs[0].Domain != "orphan.test" || repairs[0].Reason != "orphan-ssl" {
		t.Fatalf("expected [{orphan.test orphan-ssl}], got %v", repairs)
	}

	if _, err := os.Stat(filepath.Join(confD, "orphan.test.conf")); !os.IsNotExist(err) {
		t.Error("expected orphan SSL vhost to be removed")
	}
}

func TestRepairVhosts_preservesOrphanHTTPVhost(t *testing.T) {
	confD, _ := setupRepairEnv(t, "sites: []\n")

	// An HTTP vhost for an unregistered domain — harmless, should NOT be removed.
	httpConf := `server {
    listen 80;
    server_name stale.test;
    root /srv/stale/public;
}
`
	os.WriteFile(filepath.Join(confD, "stale.test.conf"), []byte(httpConf), 0644)

	repairs := RepairVhosts()

	if len(repairs) != 0 {
		t.Fatalf("expected no repairs for orphan HTTP vhost, got %v", repairs)
	}

	if _, err := os.Stat(filepath.Join(confD, "stale.test.conf")); err != nil {
		t.Error("orphan HTTP vhost should be preserved")
	}
}

func TestRepairVhosts_preservesInternalVhosts(t *testing.T) {
	confD, _ := setupRepairEnv(t, "sites: []\n")

	// Write internal vhosts that should never be removed.
	os.WriteFile(filepath.Join(confD, "_default.conf"), []byte("server {}"), 0644)
	os.WriteFile(filepath.Join(confD, "lerd.localhost.conf"), []byte("server { server_name lerd.localhost; }"), 0644)

	repairs := RepairVhosts()

	if len(repairs) != 0 {
		t.Fatalf("expected no repairs for internal vhosts, got %v", repairs)
	}
	if _, err := os.Stat(filepath.Join(confD, "_default.conf")); err != nil {
		t.Error("_default.conf should be preserved")
	}
	if _, err := os.Stat(filepath.Join(confD, "lerd.localhost.conf")); err != nil {
		t.Error("lerd.localhost.conf should be preserved")
	}
}

func TestRepairVhosts_preservesIgnoredSiteVhost(t *testing.T) {
	confD, _ := setupRepairEnv(t, `sites:
- name: ignored
  domains:
    - ignored.test
  path: /srv/ignored
  php_version: "8.4"
  ignored: true
`)

	os.WriteFile(filepath.Join(confD, "ignored.test.conf"), []byte("server { server_name ignored.test; }"), 0644)

	repairs := RepairVhosts()

	if len(repairs) != 0 {
		t.Fatalf("expected no repairs for ignored site HTTP vhost, got %v", repairs)
	}

	if _, err := os.Stat(filepath.Join(confD, "ignored.test.conf")); err != nil {
		t.Error("ignored site HTTP vhost should be preserved")
	}
}

// ── EnsureDefaultVhost ────────────────────────────────────────────────────────

// ── GenerateCustomVhost ──────────────────────────────────────────────────────

func TestGenerateCustomVhost_createsConfFile(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "nestapp", Domains: []string{"nestapp.test"}, ContainerPort: 3000}
	if err := GenerateCustomVhost(site); err != nil {
		t.Fatalf("GenerateCustomVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "nestapp.test.conf"))
	if !strings.Contains(content, "server_name nestapp.test") {
		t.Errorf("expected server_name in:\n%s", content)
	}
	if !strings.Contains(content, "proxy_pass http://$backend:3000") {
		t.Errorf("expected proxy_pass with port 3000 in:\n%s", content)
	}
	if !strings.Contains(content, "lerd-custom-nestapp") {
		t.Errorf("expected custom container name in:\n%s", content)
	}
	if strings.Contains(content, "fastcgi_pass") {
		t.Error("custom vhost should not contain fastcgi_pass")
	}
}

func TestGenerateCustomVhost_multiDomain(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "nestapp", Domains: []string{"nestapp.test", "api.test"}, ContainerPort: 3000}
	if err := GenerateCustomVhost(site); err != nil {
		t.Fatal(err)
	}
	content := readConf(t, filepath.Join(confD, "nestapp.test.conf"))
	if !strings.Contains(content, "server_name nestapp.test *.nestapp.test api.test *.api.test") {
		t.Errorf("expected all domains with wildcards in:\n%s", content)
	}
}

func TestGenerateCustomVhost_websocketHeaders(t *testing.T) {
	setupConfD(t)
	site := config.Site{Name: "app", Domains: []string{"app.test"}, ContainerPort: 8080}
	if err := GenerateCustomVhost(site); err != nil {
		t.Fatal(err)
	}
	confD := filepath.Join(os.Getenv("XDG_DATA_HOME"), "lerd", "nginx", "conf.d")
	content := readConf(t, filepath.Join(confD, "app.test.conf"))
	if !strings.Contains(content, "proxy_set_header Upgrade") {
		t.Error("expected WebSocket upgrade header")
	}
	if !strings.Contains(content, "proxy_http_version 1.1") {
		t.Error("expected HTTP/1.1 for WebSocket support")
	}
}

// ── GenerateCustomSSLVhost ───────────────────────────────────────────────────

func TestGenerateCustomSSLVhost_createsSSLConfFile(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "nestapp", Domains: []string{"nestapp.test"}, ContainerPort: 3000}
	if err := GenerateCustomSSLVhost(site); err != nil {
		t.Fatalf("GenerateCustomSSLVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "nestapp.test-ssl.conf"))
	if !strings.Contains(content, "listen 443 ssl") {
		t.Errorf("expected 443 ssl in:\n%s", content)
	}
	if !strings.Contains(content, "ssl_certificate /etc/nginx/certs/nestapp.test.crt") {
		t.Errorf("expected ssl_certificate in:\n%s", content)
	}
	if !strings.Contains(content, "proxy_pass http://$backend:3000") {
		t.Errorf("expected proxy_pass in:\n%s", content)
	}
	if !strings.Contains(content, "lerd-custom-nestapp") {
		t.Errorf("expected custom container name in:\n%s", content)
	}
	if !strings.Contains(content, "return 302 https://") {
		t.Errorf("expected HTTP redirect in:\n%s", content)
	}
	if strings.Contains(content, "fastcgi_pass") {
		t.Error("custom SSL vhost should not contain fastcgi_pass")
	}
}

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

func TestEnsureLerdVhost_proxiesUnixSocket(t *testing.T) {
	confD := setupConfD(t)
	if err := EnsureLerdVhost(); err != nil {
		t.Fatalf("EnsureLerdVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "lerd.localhost.conf"))

	// The vhost MUST proxy via the unix socket and MUST NOT mention
	// host.containers.internal. Reaching the host over TCP via netavark's
	// 169.254.1.2 alias is the failure mode this fix removes; if a future
	// refactor reintroduces it, this test catches the regression.
	want := "proxy_pass http://unix:" + config.UISocketPath()
	if !strings.Contains(content, want) {
		t.Errorf("expected %q in vhost, got:\n%s", want, content)
	}
	if strings.Contains(content, "host.containers.internal") {
		t.Errorf("vhost still references host.containers.internal:\n%s", content)
	}

	// /api/* must remain closed — the dashboard JS hits :7073 directly.
	if !strings.Contains(content, "return 444") {
		t.Errorf("expected catch-all 'return 444' in:\n%s", content)
	}
}
