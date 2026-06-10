package nginx

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// setupConfD points NginxConfD() at a temp dir via XDG_DATA_HOME and returns the
// conf.d path. XDG_CONFIG_HOME is redirected too so resolveRequestTimeout reads
// a hermetic (empty) global config instead of the developer's real one.
func setupConfD(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
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

// ── resolvePublicDir ──────────────────────────────────────────────────────────

func TestResolvePublicDir_SiteOverrideWins(t *testing.T) {
	site := config.Site{
		Name:      "myapp",
		Path:      "/srv/myapp",
		Framework: "laravel",
		PublicDir: "public_html",
	}
	if got := resolvePublicDir(site); got != "public_html" {
		t.Errorf("resolvePublicDir = %q, want public_html (site override)", got)
	}
}

func TestResolvePublicDir_FallsBackToDefault(t *testing.T) {
	site := config.Site{Name: "myapp", Path: "/srv/myapp"}
	if got := resolvePublicDir(site); got != "public" {
		t.Errorf("resolvePublicDir = %q, want public (default)", got)
	}
}

func TestGenerateVhost_customFPMUsesPerSiteContainer(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{
		Name:    "myapp",
		Domains: []string{"myapp.test"},
		Path:    "/srv/myapp",
		Runtime: "fpm-custom",
	}
	if err := GenerateVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "myapp.test.conf"))
	if !strings.Contains(content, `set $fpm "lerd-cfpm-myapp"`) {
		t.Errorf("custom-FPM vhost should fastcgi to lerd-cfpm-myapp, got:\n%s", content)
	}
	if strings.Contains(content, "lerd-php84-fpm") {
		t.Errorf("custom-FPM vhost must not reference the shared lerd-php84-fpm:\n%s", content)
	}
}

func TestGenerateVhost_plainSiteUsesSharedContainer(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "plain", Domains: []string{"plain.test"}, Path: "/srv/plain"}
	if err := GenerateVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "plain.test.conf"))
	if !strings.Contains(content, `set $fpm "lerd-php84-fpm"`) {
		t.Errorf("plain vhost should fastcgi to lerd-php84-fpm, got:\n%s", content)
	}
}

// Worktree vhosts render the fastcgi template too. With no registered site they
// must fall back to the shared container, not an empty $fpm (the regression the
// FPMContainer change could have introduced).
func TestGenerateWorktreeVhost_unknownSiteUsesSharedContainer(t *testing.T) {
	confD := setupConfD(t)
	if err := GenerateWorktreeVhost("feat.myapp.test", "/srv/myapp-feat", "8.3", "myapp", "feat"); err != nil {
		t.Fatalf("GenerateWorktreeVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "feat.myapp.test.conf"))
	if !strings.Contains(content, `set $fpm "lerd-php83-fpm"`) {
		t.Errorf("worktree vhost should fastcgi to lerd-php83-fpm, got:\n%s", content)
	}
}

func TestGenerateVhost_honoursSitePublicDir(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{
		Name:      "myapp",
		Domains:   []string{"myapp.test"},
		Path:      "/srv/myapp",
		PublicDir: "public_html",
	}
	if err := GenerateVhost(site, "8.3"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "myapp.test.conf"))
	if !strings.Contains(content, "root /srv/myapp/public_html") {
		t.Errorf("expected custom public_html doc root in:\n%s", content)
	}
}

// ── resolveRequestTimeout ─────────────────────────────────────────────────────

func TestResolveRequestTimeout_DefaultsTo60(t *testing.T) {
	setupConfD(t)
	if got := resolveRequestTimeout("/srv/nonexistent"); got != 60 {
		t.Errorf("resolveRequestTimeout = %d, want 60 (nginx default)", got)
	}
}

func TestResolveRequestTimeout_GlobalConfigWins(t *testing.T) {
	setupConfD(t)
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.Nginx.RequestTimeout = 120
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	if got := resolveRequestTimeout("/srv/nonexistent"); got != 120 {
		t.Errorf("resolveRequestTimeout = %d, want 120 (global config)", got)
	}
}

func TestResolveRequestTimeout_ProjectOverrideWins(t *testing.T) {
	setupConfD(t)
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.Nginx.RequestTimeout = 120
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	projectDir := t.TempDir()
	if err := config.SaveProjectConfig(projectDir, &config.ProjectConfig{RequestTimeout: 300}); err != nil {
		t.Fatalf("SaveProjectConfig: %v", err)
	}
	if got := resolveRequestTimeout(projectDir); got != 300 {
		t.Errorf("resolveRequestTimeout = %d, want 300 (.lerd.yaml override)", got)
	}
}

// ── request timeout rendering ─────────────────────────────────────────────────

func TestGenerateVhost_rendersDefaultRequestTimeout(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "app", Domains: []string{"app.test"}, Path: "/srv/app"}
	if err := GenerateVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "app.test.conf"))
	for _, want := range []string{"fastcgi_read_timeout 60s;", "fastcgi_send_timeout 60s;"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in:\n%s", want, content)
		}
	}
}

func TestGenerateVhost_honoursProjectRequestTimeout(t *testing.T) {
	confD := setupConfD(t)
	projectDir := t.TempDir()
	if err := config.SaveProjectConfig(projectDir, &config.ProjectConfig{RequestTimeout: 300}); err != nil {
		t.Fatalf("SaveProjectConfig: %v", err)
	}
	site := config.Site{Name: "app", Domains: []string{"app.test"}, Path: projectDir}
	if err := GenerateVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "app.test.conf"))
	if !strings.Contains(content, "fastcgi_read_timeout 300s;") {
		t.Errorf("expected fastcgi_read_timeout 300s in:\n%s", content)
	}
}

func TestGenerateSSLVhost_honoursProjectRequestTimeout(t *testing.T) {
	confD := setupConfD(t)
	projectDir := t.TempDir()
	if err := config.SaveProjectConfig(projectDir, &config.ProjectConfig{RequestTimeout: 240}); err != nil {
		t.Fatalf("SaveProjectConfig: %v", err)
	}
	site := config.Site{Name: "app", Domains: []string{"app.test"}, Path: projectDir}
	if err := GenerateSSLVhost(site, "8.4"); err != nil {
		t.Fatalf("GenerateSSLVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "app.test-ssl.conf"))
	if !strings.Contains(content, "fastcgi_read_timeout 240s;") {
		t.Errorf("expected fastcgi_read_timeout 240s in:\n%s", content)
	}
}

func TestGenerateCustomVhost_honoursProjectRequestTimeout(t *testing.T) {
	confD := setupConfD(t)
	projectDir := t.TempDir()
	if err := config.SaveProjectConfig(projectDir, &config.ProjectConfig{RequestTimeout: 180}); err != nil {
		t.Fatalf("SaveProjectConfig: %v", err)
	}
	site := config.Site{Name: "nestapp", Domains: []string{"nestapp.test"}, Path: projectDir, ContainerPort: 3000}
	if err := GenerateCustomVhost(site); err != nil {
		t.Fatalf("GenerateCustomVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "nestapp.test.conf"))
	if !strings.Contains(content, "proxy_read_timeout 180s;") {
		t.Errorf("expected proxy_read_timeout 180s in:\n%s", content)
	}
}

// ── GenerateHostProxyVhost ────────────────────────────────────────────────────

func TestGenerateHostProxyVhost_proxiesToHostPort(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "nestapp", Domains: []string{"nestapp.test"}, Path: t.TempDir(), HostPort: 3000}
	if err := GenerateHostProxyVhost(site); err != nil {
		t.Fatalf("GenerateHostProxyVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "nestapp.test.conf"))
	if !strings.Contains(content, "proxy_pass http://"+hostProxyUpstream()+":3000;") {
		t.Errorf("expected proxy_pass to host upstream port 3000 in:\n%s", content)
	}
	// HMR / WebSocket support must be present for Vite/NestJS.
	if !strings.Contains(content, `proxy_set_header Upgrade $http_upgrade;`) {
		t.Errorf("expected WebSocket upgrade header in:\n%s", content)
	}
	if !strings.Contains(content, "include /etc/nginx/custom.d/nestapp.test.conf*;") {
		t.Errorf("expected custom.d include in:\n%s", content)
	}
}

func TestGenerateHostProxyVhost_backendSSL(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "secureapp", Domains: []string{"secureapp.test"}, Path: t.TempDir(), HostPort: 8443, HostSSL: true}
	if err := GenerateHostProxyVhost(site); err != nil {
		t.Fatalf("GenerateHostProxyVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "secureapp.test.conf"))
	if !strings.Contains(content, "proxy_pass https://"+hostProxyUpstream()+":8443;") {
		t.Errorf("expected https proxy_pass for backend SSL in:\n%s", content)
	}
	if !strings.Contains(content, "proxy_ssl_verify off;") {
		t.Errorf("expected proxy_ssl_verify off for backend SSL in:\n%s", content)
	}
}

func TestGenerateHostProxySSLVhost_createsSSLConfWithCert(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "nestapp", Domains: []string{"nestapp.test"}, Path: t.TempDir(), HostPort: 3000}
	if err := GenerateHostProxySSLVhost(site); err != nil {
		t.Fatalf("GenerateHostProxySSLVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "nestapp.test-ssl.conf"))
	if !strings.Contains(content, "ssl_certificate /etc/nginx/certs/nestapp.test.crt;") {
		t.Errorf("expected ssl_certificate directive in:\n%s", content)
	}
	if !strings.Contains(content, "proxy_pass http://"+hostProxyUpstream()+":3000;") {
		t.Errorf("expected proxy_pass to host upstream in:\n%s", content)
	}
}

func TestGenerateWorktreeHostProxyVhostFor_http(t *testing.T) {
	confD := setupConfD(t)
	if err := GenerateWorktreeHostProxyVhostFor("feat-x.nestapp.test", t.TempDir(), "nestapp.test", 3101, false, false); err != nil {
		t.Fatalf("GenerateWorktreeHostProxyVhostFor: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "feat-x.nestapp.test.conf"))
	if !strings.Contains(content, "server_name feat-x.nestapp.test *.feat-x.nestapp.test") {
		t.Errorf("expected worktree server_name with wildcard, got:\n%s", content)
	}
	if !strings.Contains(content, "proxy_pass http://"+hostProxyUpstream()+":3101;") {
		t.Errorf("expected proxy_pass to the worktree port 3101, got:\n%s", content)
	}
	if strings.Contains(content, "ssl_certificate") {
		t.Errorf("HTTP worktree vhost must not emit ssl_certificate:\n%s", content)
	}
}

func TestGenerateWorktreeHostProxyVhostFor_sslUsesParentCert(t *testing.T) {
	confD := setupConfD(t)
	if err := GenerateWorktreeHostProxyVhostFor("feat-x.nestapp.test", t.TempDir(), "nestapp.test", 3101, false, true); err != nil {
		t.Fatalf("GenerateWorktreeHostProxyVhostFor (ssl): %v", err)
	}
	content := readConf(t, filepath.Join(confD, "feat-x.nestapp.test.conf"))
	// Worktree SSL must use the parent's wildcard cert, never its own.
	if !strings.Contains(content, "ssl_certificate /etc/nginx/certs/nestapp.test.crt;") {
		t.Errorf("expected parent cert, got:\n%s", content)
	}
	if strings.Contains(content, "feat-x.nestapp.test.crt") {
		t.Errorf("worktree vhost must not reference its own cert:\n%s", content)
	}
	if !strings.Contains(content, "proxy_pass http://"+hostProxyUpstream()+":3101;") {
		t.Errorf("expected proxy_pass to the worktree port, got:\n%s", content)
	}
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
	if err := GenerateWorktreeVhost("feat-x.myapp.test", "/srv/myapp-feat", "8.3", "myapp", "feat-x"); err != nil {
		t.Fatalf("GenerateWorktreeVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "feat-x.myapp.test.conf"))
	if !strings.Contains(content, "server_name feat-x.myapp.test") {
		t.Errorf("expected worktree domain in:\n%s", content)
	}
	if !strings.Contains(content, "*.feat-x.myapp.test") {
		t.Errorf("expected wildcard server_name for worktree subdomains in:\n%s", content)
	}
	if !strings.Contains(content, "root /srv/myapp-feat/public") {
		t.Errorf("expected worktree path in:\n%s", content)
	}
}

// ── GenerateWorktreeSSLVhost ──────────────────────────────────────────────────

func TestGenerateWorktreeSSLVhost_usesParentCert(t *testing.T) {
	confD := setupConfD(t)
	if err := GenerateWorktreeSSLVhost("feat-x.myapp.test", "/srv/myapp-feat", "8.3", "myapp.test", "myapp", "feat-x"); err != nil {
		t.Fatalf("GenerateWorktreeSSLVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "feat-x.myapp.test.conf"))
	if !strings.Contains(content, "server_name feat-x.myapp.test") {
		t.Errorf("expected worktree domain in:\n%s", content)
	}
	if !strings.Contains(content, "*.feat-x.myapp.test") {
		t.Errorf("expected wildcard server_name for worktree subdomains in:\n%s", content)
	}
	// Must use parent domain's cert (wildcard *.myapp.test), not feat-x.myapp.test
	if !strings.Contains(content, "myapp.test.crt") {
		t.Errorf("expected parent domain cert in:\n%s", content)
	}
	if strings.Contains(content, "feat-x.myapp.test.crt") {
		t.Error("worktree vhost must not reference its own cert file")
	}
}

// ── GenerateWorktreeVhostFor ──────────────────────────────────────────────────

// TestGenerateWorktreeVhostFor_routesByFlag pins the behaviour of the
// shared wrapper that callers (scanWorktrees, syncWorktree, migrateTLD)
// use to avoid repeating the secured-vs-plain branch around the two
// underlying generators.
func TestGenerateWorktreeVhostFor_routesByFlag(t *testing.T) {
	confD := setupConfD(t)

	if err := GenerateWorktreeVhostFor("feat-x.myapp.test", "/srv/myapp-feat", "8.3", "myapp.test", "myapp", "feat-x", false); err != nil {
		t.Fatalf("HTTP wrapper: %v", err)
	}
	httpContent := readConf(t, filepath.Join(confD, "feat-x.myapp.test.conf"))
	if strings.Contains(httpContent, "ssl_certificate") {
		t.Error("HTTP variant must not emit ssl_certificate")
	}

	// Re-run with secured=true; the same conf path should now point at
	// the parent's wildcard cert.
	if err := GenerateWorktreeVhostFor("feat-x.myapp.test", "/srv/myapp-feat", "8.3", "myapp.test", "myapp", "feat-x", true); err != nil {
		t.Fatalf("HTTPS wrapper: %v", err)
	}
	sslContent := readConf(t, filepath.Join(confD, "feat-x.myapp.test.conf"))
	if !strings.Contains(sslContent, "myapp.test.crt") {
		t.Errorf("HTTPS variant should reference parent cert, got:\n%s", sslContent)
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
	// Sentinel hash must be written alongside so subsequent runs can
	// distinguish lerd-managed content from a user edit.
	sentinel := filepath.Join(confD, "_default.conf"+defaultVhostManagedHashSuffix)
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("expected sentinel at %s", sentinel)
	}
}

func TestEnsureDefaultVhost_preservesUserEdits(t *testing.T) {
	confD := setupConfD(t)
	// First pass: lerd writes the canonical content + sentinel.
	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("first EnsureDefaultVhost: %v", err)
	}
	// User patches the file.
	path := filepath.Join(confD, "_default.conf")
	userEdited := []byte("# hand-tuned for staging\nserver {\n    listen 80;\n    ssl_reject_handshake off;\n}\n")
	if err := os.WriteFile(path, userEdited, 0644); err != nil {
		t.Fatalf("simulating user edit: %v", err)
	}
	// Second pass: should NOT overwrite.
	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("second EnsureDefaultVhost: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-reading conf: %v", err)
	}
	if string(got) != string(userEdited) {
		t.Errorf("user edit was overwritten\nwant:\n%s\ngot:\n%s", userEdited, got)
	}
}

func TestEnsureDefaultVhost_idempotentWhenUntouched(t *testing.T) {
	confD := setupConfD(t)
	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("first EnsureDefaultVhost: %v", err)
	}
	path := filepath.Join(confD, "_default.conf")
	before, _ := os.ReadFile(path)
	// Second pass with no user edits: bytes should match exactly.
	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("second EnsureDefaultVhost: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Errorf("re-run without edits should be a no-op\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestEnsureDefaultVhost_templateChangeAutoUpdatesWhenUnedited(t *testing.T) {
	confD := setupConfD(t)
	// Simulate a previously-installed lerd that wrote OLD content + a
	// sentinel matching that old content. Reaching EnsureDefaultVhost
	// today should detect the template-vs-on-disk drift and rewrite.
	if err := os.MkdirAll(confD, 0755); err != nil {
		t.Fatal(err)
	}
	stale := []byte("# old lerd template, before the latest binary\nserver { listen 80; }\n")
	path := filepath.Join(confD, "_default.conf")
	if err := os.WriteFile(path, stale, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+defaultVhostManagedHashSuffix, []byte(contentHashHex(stale)), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("EnsureDefaultVhost: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "default_server") {
		t.Errorf("expected lerd to overwrite stale content with the current template, got:\n%s", got)
	}
}

func TestEnsureDefaultVhost_recoversManagementWhenSentinelMissingButContentMatches(t *testing.T) {
	confD := setupConfD(t)
	// Simulate a sentinel-write crash from a prior run: the conf is lerd's
	// canonical bytes, but the sentinel file never made it to disk. The
	// next run must reclaim management (write the sentinel) rather than
	// silently treat the file as user-managed.
	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("first EnsureDefaultVhost: %v", err)
	}
	path := filepath.Join(confD, "_default.conf")
	sentinel := path + defaultVhostManagedHashSuffix
	if err := os.Remove(sentinel); err != nil {
		t.Fatalf("removing sentinel: %v", err)
	}

	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("recovery EnsureDefaultVhost: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("expected sentinel to be recreated when on-disk matches canonical, got %v", err)
	}
}

func TestEnsureDefaultVhost_removingFileResetsManagement(t *testing.T) {
	confD := setupConfD(t)
	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("first EnsureDefaultVhost: %v", err)
	}
	path := filepath.Join(confD, "_default.conf")
	sentinel := path + defaultVhostManagedHashSuffix
	// User deletes the file (and may have left the sentinel; either way,
	// lerd should regenerate the catch-all on the next run).
	if err := os.Remove(path); err != nil {
		t.Fatalf("removing conf: %v", err)
	}
	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("regenerate after delete: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected lerd to recreate the file after user removed it: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("expected sentinel to be re-created alongside: %v", err)
	}
}

func TestWriteFileAtomic_writesContentAndLeavesNoTempBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed.conf")
	if err := WriteFileAtomic(path, []byte("server { listen 80; }\n"), 0644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "server { listen 80; }\n" {
		t.Fatalf("content mismatch or read err: got=%q err=%v", got, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected sibling .tmp to be cleaned up, stat err=%v", err)
	}
}

func TestWriteFileAtomic_preservesExistingFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed.conf")
	if err := os.WriteFile(path, []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("v2\n"), 0644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("existing chmod was reset: got mode %o, want 0600", got)
	}
}

func TestWriteFileAtomic_usesCallerModeForNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.conf")
	if err := WriteFileAtomic(path, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Errorf("fresh-create mode: got %o, want 0644", got)
	}
}

func TestEnsureDefaultVhost_leavesNoTempFilesInConfD(t *testing.T) {
	confD := setupConfD(t)
	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("EnsureDefaultVhost: %v", err)
	}
	// Second pass through the "on-disk matches what we last wrote" branch.
	if err := EnsureDefaultVhost(); err != nil {
		t.Fatalf("re-run EnsureDefaultVhost: %v", err)
	}
	entries, err := os.ReadDir(confD)
	if err != nil {
		t.Fatalf("readdir conf.d: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("unexpected leftover temp file in conf.d: %s", e.Name())
		}
	}
}

func TestEnsureLerdVhost_linuxProxiesUnixSocket(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Linux uses the unix socket vhost; macOS uses TCP via host.containers.internal")
	}
	confD := setupConfD(t)
	if err := EnsureLerdVhost(); err != nil {
		t.Fatalf("EnsureLerdVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "lerd.localhost.conf"))

	// On Linux the vhost MUST proxy via the unix socket. host.containers.internal
	// is the failure mode the fix removes; if a future refactor reintroduces
	// it on Linux, this test catches the regression.
	want := "proxy_pass http://unix:" + config.UISocketPath()
	if !strings.Contains(content, want) {
		t.Errorf("expected %q in vhost, got:\n%s", want, content)
	}
	if strings.Contains(content, "host.containers.internal") {
		t.Errorf("vhost still references host.containers.internal on Linux:\n%s", content)
	}

	// /api/* must remain closed — the dashboard JS hits :7073 directly.
	if !strings.Contains(content, "return 444") {
		t.Errorf("expected catch-all 'return 444' in:\n%s", content)
	}
}

// ── Forwarded headers & custom.d include hook ────────────────────────────────

func TestEnsureForwardedConf_writesMapBlocks(t *testing.T) {
	confD := setupConfD(t)
	if err := EnsureForwardedConf(); err != nil {
		t.Fatalf("EnsureForwardedConf: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "_forwarded.conf"))
	if !strings.Contains(content, "map $http_x_forwarded_host $real_forwarded_host") {
		t.Errorf("expected real_forwarded_host map, got:\n%s", content)
	}
	if !strings.Contains(content, "map $http_x_forwarded_proto $real_forwarded_proto") {
		t.Errorf("expected real_forwarded_proto map, got:\n%s", content)
	}
	if !strings.Contains(content, "map $http_x_forwarded_port $real_forwarded_port") {
		t.Errorf("expected real_forwarded_port map, got:\n%s", content)
	}
}

func TestEnsureCustomD_createsDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := EnsureCustomD(); err != nil {
		t.Fatalf("EnsureCustomD: %v", err)
	}
	customD := filepath.Join(tmp, "lerd", "nginx", "custom.d")
	info, err := os.Stat(customD)
	if err != nil {
		t.Fatalf("expected custom.d dir at %s: %v", customD, err)
	}
	if !info.IsDir() {
		t.Errorf("expected %s to be a directory", customD)
	}
}

func TestGenerateVhost_includesForwardedFastcgiParams(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "fwd", Domains: []string{"fwd.test"}, Path: "/srv/fwd"}
	if err := GenerateVhost(site, "8.3"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "fwd.test.conf"))
	wants := []string{
		"fastcgi_param HTTP_HOST $real_forwarded_host",
		"fastcgi_param SERVER_NAME $real_forwarded_host",
		"fastcgi_param HTTP_X_FORWARDED_HOST $real_forwarded_host",
		"fastcgi_param HTTP_X_FORWARDED_PROTO $real_forwarded_proto",
		"fastcgi_param HTTP_X_FORWARDED_PORT $real_forwarded_port",
		"fastcgi_param HTTP_X_REAL_IP $remote_addr",
		"fastcgi_param HTTP_X_FORWARDED_FOR $remote_addr",
	}
	for _, w := range wants {
		if !strings.Contains(content, w) {
			t.Errorf("vhost missing %q in:\n%s", w, content)
		}
	}
}

func TestGenerateSSLVhost_includesForwardedFastcgiParams(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "fwd", Domains: []string{"fwd.test"}, Path: "/srv/fwd"}
	if err := GenerateSSLVhost(site, "8.3"); err != nil {
		t.Fatalf("GenerateSSLVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "fwd.test-ssl.conf"))
	if !strings.Contains(content, "fastcgi_param HTTP_X_FORWARDED_PROTO $real_forwarded_proto") {
		t.Errorf("SSL vhost missing X-Forwarded-Proto fastcgi_param in:\n%s", content)
	}
	if !strings.Contains(content, "fastcgi_param HTTPS on") {
		t.Errorf("SSL vhost must keep HTTPS flag in:\n%s", content)
	}
}

func TestGenerateVhost_includesCustomDHook(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "fwd", Domains: []string{"fwd.test"}, Path: "/srv/fwd"}
	if err := GenerateVhost(site, "8.3"); err != nil {
		t.Fatal(err)
	}
	content := readConf(t, filepath.Join(confD, "fwd.test.conf"))
	if !strings.Contains(content, "include /etc/nginx/custom.d/fwd.test.conf*;") {
		t.Errorf("expected custom.d include hook with trailing * for missing-file tolerance, got:\n%s", content)
	}
}

func TestGenerateSSLVhost_includesCustomDHook(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "fwd", Domains: []string{"fwd.test"}, Path: "/srv/fwd"}
	if err := GenerateSSLVhost(site, "8.3"); err != nil {
		t.Fatal(err)
	}
	content := readConf(t, filepath.Join(confD, "fwd.test-ssl.conf"))
	if !strings.Contains(content, "include /etc/nginx/custom.d/fwd.test.conf*;") {
		t.Errorf("expected custom.d include hook in SSL vhost, got:\n%s", content)
	}
}

func TestGenerateCustomVhost_includesCustomDHookAndForwardedHost(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "nestapp", Domains: []string{"nestapp.test"}, ContainerPort: 3000}
	if err := GenerateCustomVhost(site); err != nil {
		t.Fatal(err)
	}
	content := readConf(t, filepath.Join(confD, "nestapp.test.conf"))
	if !strings.Contains(content, "proxy_set_header Host $real_forwarded_host") {
		t.Errorf("custom vhost should forward Host via $real_forwarded_host, got:\n%s", content)
	}
	if !strings.Contains(content, "proxy_set_header X-Forwarded-Proto $real_forwarded_proto") {
		t.Errorf("custom vhost missing forwarded-proto header, got:\n%s", content)
	}
	if !strings.Contains(content, "include /etc/nginx/custom.d/nestapp.test.conf*;") {
		t.Errorf("custom vhost missing custom.d include, got:\n%s", content)
	}
}

func TestGenerateCustomSSLVhost_includesCustomDHookAndForwardedHost(t *testing.T) {
	confD := setupConfD(t)
	site := config.Site{Name: "nestapp", Domains: []string{"nestapp.test"}, ContainerPort: 3000}
	if err := GenerateCustomSSLVhost(site); err != nil {
		t.Fatal(err)
	}
	content := readConf(t, filepath.Join(confD, "nestapp.test-ssl.conf"))
	if !strings.Contains(content, "proxy_set_header Host $real_forwarded_host") {
		t.Errorf("custom SSL vhost should forward Host via $real_forwarded_host, got:\n%s", content)
	}
	if !strings.Contains(content, "include /etc/nginx/custom.d/nestapp.test.conf*;") {
		t.Errorf("custom SSL vhost missing custom.d include, got:\n%s", content)
	}
}

func TestEnsureNginxConfig_writesForwardedAndCustomD(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := EnsureNginxConfig(); err != nil {
		t.Fatalf("EnsureNginxConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "lerd", "nginx", "conf.d", "_forwarded.conf")); err != nil {
		t.Errorf("expected _forwarded.conf to be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "lerd", "nginx", "custom.d")); err != nil {
		t.Errorf("expected custom.d dir to be created: %v", err)
	}
	// http.d dir + http.d include line in the rendered nginx.conf are the
	// preconditions the http config editor heals on its first POST. Anchor
	// both here so the heal stays a no-op once it has run (and so a
	// regression to either side surfaces as a unit failure rather than as
	// a silent-write-on-stale-install bug).
	if _, err := os.Stat(filepath.Join(tmp, "lerd", "nginx", "http.d")); err != nil {
		t.Errorf("expected http.d dir to be created: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(tmp, "lerd", "nginx", "nginx.conf"))
	if err != nil {
		t.Fatalf("read rendered nginx.conf: %v", err)
	}
	if !strings.Contains(string(body), "include /etc/nginx/http.d/*.conf;") {
		t.Errorf("rendered nginx.conf missing http.d include directive, got:\n%s", body)
	}
}

// TestEnsureNginxConfigServerNamesHashBucket guards against issue #455: a
// worktree vhost emits a long "<branch>.<site>.test *.<branch>.<site>.test"
// server_name that overflows nginx's default server_names_hash_bucket_size of
// 64, crashing nginx for every site. The rendered global nginx.conf must raise
// the bucket/max sizes so long branch names always fit.
func TestEnsureNginxConfigServerNamesHashBucket(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := EnsureNginxConfig(); err != nil {
		t.Fatalf("EnsureNginxConfig: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(tmp, "lerd", "nginx", "nginx.conf"))
	if err != nil {
		t.Fatalf("read rendered nginx.conf: %v", err)
	}
	if !strings.Contains(string(body), "server_names_hash_bucket_size 256;") {
		t.Errorf("rendered nginx.conf missing server_names_hash_bucket_size directive, got:\n%s", body)
	}
	if !strings.Contains(string(body), "server_names_hash_max_size 1024;") {
		t.Errorf("rendered nginx.conf missing server_names_hash_max_size directive, got:\n%s", body)
	}
}

func TestEnsureForwardedConf_rewrittenOnEachCall(t *testing.T) {
	confD := setupConfD(t)
	if err := EnsureForwardedConf(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(confD, "_forwarded.conf")
	if err := os.WriteFile(path, []byte("tampered"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureForwardedConf(); err != nil {
		t.Fatalf("second EnsureForwardedConf: %v", err)
	}
	content := readConf(t, path)
	if strings.Contains(content, "tampered") {
		t.Errorf("_forwarded.conf must be rewritten on every ensure call so new variables reach existing installs, got:\n%s", content)
	}
}

func TestEnsureLerdVhost_darwinProxiesHostContainersInternal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS uses TCP via host.containers.internal because unix sockets don't traverse the podman-machine virtio-fs boundary as functional sockets")
	}
	confD := setupConfD(t)
	if err := EnsureLerdVhost(); err != nil {
		t.Fatalf("EnsureLerdVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "lerd.localhost.conf"))

	// On macOS the vhost MUST proxy via TCP to host.containers.internal:7073
	// and MUST inject the X-Lerd-Trust header so lerd-ui's gate sees the
	// proxied request as loopback (it arrives via the bridge, not 127.0.0.1).
	if !strings.Contains(content, "proxy_pass http://host.containers.internal:7073") {
		t.Errorf("expected host.containers.internal proxy_pass in macOS vhost:\n%s", content)
	}
	if !strings.Contains(content, "X-Lerd-Trust") {
		t.Errorf("macOS vhost must inject X-Lerd-Trust header:\n%s", content)
	}
	if strings.Contains(content, "unix:") {
		t.Errorf("macOS vhost must not use a unix socket (won't traverse the VM boundary):\n%s", content)
	}
	if !strings.Contains(content, "return 444") {
		t.Errorf("expected catch-all 'return 444' in:\n%s", content)
	}
}
