package siteops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
)

// setupHostProxyEnv creates a temp NestJS-style project whose .lerd.yaml
// declares a proxy: block, with XDG overrides so config/nginx writes stay in
// the temp dir.
func setupHostProxyEnv(t *testing.T) (projectDir, confD string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))

	projectDir = filepath.Join(tmp, "nestjs-app")
	os.MkdirAll(projectDir, 0755)

	os.WriteFile(filepath.Join(projectDir, ".lerd.yaml"), []byte(`domains:
  - nestapp
proxy:
  command: npm run start:dev
  port: 3000
services:
  - redis
`), 0644)

	os.WriteFile(filepath.Join(projectDir, "package.json"), []byte(`{
  "name": "nestjs-app",
  "scripts": {"start:dev": "nest start --watch"},
  "dependencies": {"@nestjs/core": "^10"}
}
`), 0644)

	confD = filepath.Join(tmp, "lerd", "nginx", "conf.d")
	return projectDir, confD
}

func TestHostProxy_ProjectConfigParsing(t *testing.T) {
	projectDir, _ := setupHostProxyEnv(t)

	proj, err := config.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}
	if proj.Proxy == nil {
		t.Fatal("expected proxy config, got nil")
	}
	if proj.Proxy.Port != 3000 {
		t.Errorf("Proxy.Port = %d, want 3000", proj.Proxy.Port)
	}
	if proj.Proxy.Command != "npm run start:dev" {
		t.Errorf("Proxy.Command = %q", proj.Proxy.Command)
	}
	if proj.Container != nil {
		t.Error("host-proxy project should not have a container config")
	}
}

func TestHostProxy_SiteRegistration(t *testing.T) {
	projectDir, _ := setupHostProxyEnv(t)
	proj, _ := config.LoadProjectConfig(projectDir)

	site := config.Site{
		Name:        "nestjs-app",
		Domains:     []string{"nestapp.test"},
		Path:        projectDir,
		HostPort:    proj.Proxy.Port,
		HostCommand: proj.Proxy.Command,
	}
	if err := config.AddSite(site); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	got, err := config.FindSite("nestjs-app")
	if err != nil {
		t.Fatalf("FindSite: %v", err)
	}
	if !got.IsHostProxy() {
		t.Error("expected IsHostProxy() = true")
	}
	if got.IsCustomContainer() {
		t.Error("host-proxy site must not report IsCustomContainer()")
	}
	if got.PHPVersion != "" {
		t.Errorf("PHPVersion should be empty for host-proxy site, got %q", got.PHPVersion)
	}
}

func TestHostProxy_VhostGeneration(t *testing.T) {
	_, confD := setupHostProxyEnv(t)

	site := config.Site{Name: "nestjs-app", Domains: []string{"nestapp.test"}, HostPort: 3000}
	if err := nginx.GenerateHostProxyVhost(site); err != nil {
		t.Fatalf("GenerateHostProxyVhost: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(confD, "nestapp.test.conf"))
	if err != nil {
		t.Fatalf("reading vhost: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "server_name nestapp.test *.nestapp.test") {
		t.Error("missing server_name with wildcard")
	}
	if !strings.Contains(s, ":3000;") {
		t.Error("missing proxy_pass to port 3000")
	}
	if !strings.Contains(s, "proxy_set_header Upgrade") {
		t.Error("missing WebSocket Upgrade header")
	}
	if strings.Contains(s, "fastcgi_pass") || strings.Contains(s, "index.php") {
		t.Error("host-proxy vhost should not reference PHP")
	}
}
