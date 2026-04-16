package siteops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
)

// setupCustomContainerEnv creates a temp environment with XDG overrides and
// stubs for podman functions that would require a running system.
func setupCustomContainerEnv(t *testing.T) (projectDir, confD string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))

	// Stub out functions that call podman/systemd.
	origWriteUnit := podman.WriteContainerUnitFn
	origDaemonReload := podman.DaemonReloadFn
	origAfterUnitChange := podman.AfterUnitChange
	t.Cleanup(func() {
		podman.WriteContainerUnitFn = origWriteUnit
		podman.DaemonReloadFn = origDaemonReload
		podman.AfterUnitChange = origAfterUnitChange
	})

	// Write quadlets to a temp dir instead of the real quadlet dir.
	quadletDir := filepath.Join(tmp, "config", "containers", "systemd")
	os.MkdirAll(quadletDir, 0755)
	podman.WriteContainerUnitFn = func(name, content string) error {
		return os.WriteFile(filepath.Join(quadletDir, name+".container"), []byte(content), 0644)
	}
	podman.DaemonReloadFn = func() error { return nil }
	podman.AfterUnitChange = nil

	// Create the NestJS project directory with Containerfile.lerd and .lerd.yaml.
	projectDir = filepath.Join(tmp, "nestjs-app")
	os.MkdirAll(projectDir, 0755)

	os.WriteFile(filepath.Join(projectDir, "Containerfile.lerd"), []byte(`FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
CMD ["npm", "run", "start:dev"]
`), 0644)

	os.WriteFile(filepath.Join(projectDir, ".lerd.yaml"), []byte(`domains:
  - nestapp
container:
  port: 3000
services:
  - redis
custom_workers:
  dev-server:
    label: Dev Server
    command: npm run start:dev
    restart: always
`), 0644)

	os.WriteFile(filepath.Join(projectDir, "package.json"), []byte(`{
  "name": "nestjs-app",
  "version": "1.0.0",
  "scripts": {
    "start:dev": "nest start --watch"
  }
}
`), 0644)

	confD = filepath.Join(tmp, "lerd", "nginx", "conf.d")
	return projectDir, confD
}

func TestCustomContainer_NestJS_ProjectConfigParsing(t *testing.T) {
	projectDir, _ := setupCustomContainerEnv(t)

	proj, err := config.LoadProjectConfig(projectDir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: %v", err)
	}

	if proj.Container == nil {
		t.Fatal("expected container config, got nil")
	}
	if proj.Container.Port != 3000 {
		t.Errorf("Container.Port = %d, want 3000", proj.Container.Port)
	}
	if len(proj.Domains) != 1 || proj.Domains[0] != "nestapp" {
		t.Errorf("Domains = %v, want [nestapp]", proj.Domains)
	}
	if len(proj.CustomWorkers) != 1 {
		t.Fatalf("CustomWorkers count = %d, want 1", len(proj.CustomWorkers))
	}
	w, ok := proj.CustomWorkers["dev-server"]
	if !ok {
		t.Fatal("expected custom worker dev-server")
	}
	if w.Command != "npm run start:dev" {
		t.Errorf("worker command = %q", w.Command)
	}
}

func TestCustomContainer_NestJS_SiteRegistration(t *testing.T) {
	projectDir, _ := setupCustomContainerEnv(t)

	proj, _ := config.LoadProjectConfig(projectDir)

	site := config.Site{
		Name:          "nestjs-app",
		Domains:       []string{"nestapp.test"},
		Path:          projectDir,
		ContainerPort: proj.Container.Port,
	}

	if err := config.AddSite(site); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	// Verify the site is registered and marked as custom container.
	got, err := config.FindSite("nestjs-app")
	if err != nil {
		t.Fatalf("FindSite: %v", err)
	}
	if !got.IsCustomContainer() {
		t.Error("expected IsCustomContainer() = true")
	}
	if got.ContainerPort != 3000 {
		t.Errorf("ContainerPort = %d, want 3000", got.ContainerPort)
	}
	if got.PHPVersion != "" {
		t.Errorf("PHPVersion should be empty for custom container site, got %q", got.PHPVersion)
	}
}

func TestCustomContainer_NestJS_ContainerfileDetection(t *testing.T) {
	projectDir, _ := setupCustomContainerEnv(t)

	if !podman.HasContainerfile(projectDir) {
		t.Error("expected HasContainerfile = true for NestJS project")
	}

	proj, _ := config.LoadProjectConfig(projectDir)
	cf := podman.ResolveContainerfile(projectDir, proj.Container)
	if !strings.HasSuffix(cf, "Containerfile.lerd") {
		t.Errorf("ResolveContainerfile = %q, expected Containerfile.lerd suffix", cf)
	}
	if _, err := os.Stat(cf); err != nil {
		t.Errorf("Containerfile should exist at %s", cf)
	}
}

func TestCustomContainer_NestJS_NamingConventions(t *testing.T) {
	if got := podman.CustomContainerName("nestjs-app"); got != "lerd-custom-nestjs-app" {
		t.Errorf("CustomContainerName = %q", got)
	}
	if got := podman.CustomImageName("nestjs-app"); got != "lerd-custom-nestjs-app:local" {
		t.Errorf("CustomImageName = %q", got)
	}
}

func TestCustomContainer_NestJS_QuadletGeneration(t *testing.T) {
	projectDir, _ := setupCustomContainerEnv(t)

	content := podman.GenerateCustomContainerQuadlet("nestjs-app", projectDir, 3000)

	checks := []struct {
		label, substr string
	}{
		{"image", "Image=lerd-custom-nestjs-app:local"},
		{"container name", "ContainerName=lerd-custom-nestjs-app"},
		{"network", "Network=lerd"},
		{"project mount", "Volume=" + projectDir + ":" + projectDir + ":rw"},
		{"hosts mount", "/etc/hosts:ro,z"},
		{"description", "Lerd custom container (nestjs-app)"},
	}
	for _, c := range checks {
		if !strings.Contains(content, c.substr) {
			t.Errorf("%s: missing %q in quadlet", c.label, c.substr)
		}
	}

	// Must NOT contain PHP-specific things.
	if strings.Contains(content, "xdebug") || strings.Contains(content, "php-fpm") {
		t.Error("custom container quadlet should not reference PHP/xdebug")
	}
}

func TestCustomContainer_NestJS_VhostGeneration_HTTP(t *testing.T) {
	_, confD := setupCustomContainerEnv(t)

	site := config.Site{
		Name:          "nestjs-app",
		Domains:       []string{"nestapp.test"},
		ContainerPort: 3000,
	}

	if err := nginx.GenerateCustomVhost(site); err != nil {
		t.Fatalf("GenerateCustomVhost: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(confD, "nestapp.test.conf"))
	if err != nil {
		t.Fatalf("reading vhost: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "server_name nestapp.test *.nestapp.test") {
		t.Error("missing server_name with wildcard")
	}
	if !strings.Contains(s, "proxy_pass http://$backend:3000") {
		t.Error("missing proxy_pass to port 3000")
	}
	if !strings.Contains(s, `"lerd-custom-nestjs-app"`) {
		t.Error("missing custom container name in proxy backend")
	}
	if strings.Contains(s, "fastcgi_pass") {
		t.Error("HTTP custom vhost should not have fastcgi_pass")
	}
	if strings.Contains(s, "index.php") {
		t.Error("custom vhost should not reference PHP")
	}
	// WebSocket support
	if !strings.Contains(s, "proxy_set_header Upgrade") {
		t.Error("missing WebSocket Upgrade header")
	}
}

func TestCustomContainer_NestJS_VhostGeneration_HTTPS(t *testing.T) {
	_, confD := setupCustomContainerEnv(t)

	site := config.Site{
		Name:          "nestjs-app",
		Domains:       []string{"nestapp.test"},
		ContainerPort: 3000,
	}

	if err := nginx.GenerateCustomSSLVhost(site); err != nil {
		t.Fatalf("GenerateCustomSSLVhost: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(confD, "nestapp.test-ssl.conf"))
	if err != nil {
		t.Fatalf("reading SSL vhost: %v", err)
	}
	s := string(content)

	if !strings.Contains(s, "listen 443 ssl") {
		t.Error("missing SSL listener")
	}
	if !strings.Contains(s, "ssl_certificate /etc/nginx/certs/nestapp.test.crt") {
		t.Error("missing ssl_certificate")
	}
	if !strings.Contains(s, "return 302 https://") {
		t.Error("missing HTTP-to-HTTPS redirect")
	}
	if !strings.Contains(s, "proxy_pass http://$backend:3000") {
		t.Error("missing proxy_pass in SSL vhost")
	}
	if strings.Contains(s, "fastcgi_pass") {
		t.Error("SSL custom vhost should not have fastcgi_pass")
	}
}

func TestCustomContainer_NestJS_UnlinkCleanup(t *testing.T) {
	projectDir, confD := setupCustomContainerEnv(t)

	site := config.Site{
		Name:          "nestjs-app",
		Domains:       []string{"nestapp.test"},
		Path:          projectDir,
		ContainerPort: 3000,
	}

	// Register the site.
	if err := config.AddSite(site); err != nil {
		t.Fatal(err)
	}

	// Write the vhost so RemoveVhost has something to delete.
	if err := nginx.GenerateCustomVhost(site); err != nil {
		t.Fatal(err)
	}
	vhostPath := filepath.Join(confD, "nestapp.test.conf")
	if _, err := os.Stat(vhostPath); err != nil {
		t.Fatal("vhost should exist before unlink")
	}

	// Write the quadlet so removal can clean it up.
	if err := podman.WriteCustomContainerQuadlet("nestjs-app", projectDir, 3000); err != nil {
		t.Fatal(err)
	}

	// Stub out functions called during unlink that need podman.
	origStopUnit := podman.UnitLifecycle
	podman.UnitLifecycle = &noopLifecycle{}
	t.Cleanup(func() { podman.UnitLifecycle = origStopUnit })

	// We can't call the full UnlinkSiteCore because it calls nginx.Reload,
	// podman.WriteContainerHosts, etc. But we can verify the pieces.

	// Verify the site is removed from registry.
	if err := config.RemoveSite("nestjs-app"); err != nil {
		t.Fatal(err)
	}
	_, err := config.FindSite("nestjs-app")
	if err == nil {
		t.Error("site should not be found after removal")
	}

	// Verify vhost removal.
	if err := nginx.RemoveVhost("nestapp.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(vhostPath); !os.IsNotExist(err) {
		t.Error("vhost should be removed after unlink")
	}

	// Verify quadlet removal.
	if err := podman.RemoveCustomContainerQuadlet("nestjs-app"); err != nil {
		t.Fatal(err)
	}
}

func TestCustomContainer_NestJS_IsNotPHPSite(t *testing.T) {
	site := config.Site{
		Name:          "nestjs-app",
		Domains:       []string{"nestapp.test"},
		ContainerPort: 3000,
	}

	if !site.IsCustomContainer() {
		t.Error("NestJS site should be a custom container")
	}
	if site.PHPVersion != "" {
		t.Error("NestJS site should not have a PHP version")
	}
	if site.Framework != "" {
		t.Error("NestJS site should not have a framework")
	}
}

// noopLifecycle is a stub for podman.UnitLifecycle that does nothing.
type noopLifecycle struct{}

func (n *noopLifecycle) Start(name string) error                { return nil }
func (n *noopLifecycle) Stop(name string) error                 { return nil }
func (n *noopLifecycle) Restart(name string) error              { return nil }
func (n *noopLifecycle) UnitStatus(name string) (string, error) { return "inactive", nil }
