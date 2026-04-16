package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/siteops"
)

// ── siteNameAndDomain ────────────────────────────────────────────────────────

func TestSiteNameAndDomain(t *testing.T) {
	cases := []struct {
		dirName    string
		tld        string
		wantName   string
		wantDomain string
	}{
		{"myapp", "test", "myapp", "myapp.test"},
		{"MyApp", "test", "myapp", "myapp.test"},
		{"admin.astrolov.com", "test", "admin-astrolov", "admin-astrolov.test"},
		{"my.project.io", "test", "my-project", "my-project.test"},
		{"shop.co", "test", "shop", "shop.test"},
		{"api.dev", "test", "api", "api.test"},
		{"plain", "local", "plain", "plain.local"},
		{"has.dots.net", "test", "has-dots", "has-dots.test"},
	}
	for _, c := range cases {
		gotName, gotDomain := siteops.SiteNameAndDomain(c.dirName, c.tld)
		if gotName != c.wantName {
			t.Errorf("siteNameAndDomain(%q, %q) name = %q, want %q", c.dirName, c.tld, gotName, c.wantName)
		}
		if gotDomain != c.wantDomain {
			t.Errorf("siteNameAndDomain(%q, %q) domain = %q, want %q", c.dirName, c.tld, gotDomain, c.wantDomain)
		}
	}
}

// ── freeSiteName ─────────────────────────────────────────────────────────────

// setupSitesYAML writes a sites.yaml into a temp XDG_DATA_HOME so that
// config.FindSite reads from it instead of the real user config.
func setupSitesYAML(t *testing.T, yaml string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	dir := filepath.Join(tmp, "lerd")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sites.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFreeSiteName_unused(t *testing.T) {
	setupSitesYAML(t, "sites: []\n")
	got := freeSiteName("myapp", "/projects/myapp")
	if got != "myapp" {
		t.Errorf("got %q, want %q", got, "myapp")
	}
}

func TestFreeSiteName_samePath_rerelink(t *testing.T) {
	setupSitesYAML(t, `sites:
  - name: myapp
    domains:
      - myapp.test
    path: /projects/myapp
    php_version: "8.3"
    node_version: "22"
`)
	// Same path → should return the same name (re-link / upsert)
	got := freeSiteName("myapp", "/projects/myapp")
	if got != "myapp" {
		t.Errorf("got %q, want %q", got, "myapp")
	}
}

func TestFreeSiteName_collision_differentPath(t *testing.T) {
	setupSitesYAML(t, `sites:
  - name: myapp
    domains:
      - myapp.test
    path: /projects/other-myapp
    php_version: "8.3"
    node_version: "22"
`)
	// "myapp" is taken by a different path → should get "myapp-2"
	got := freeSiteName("myapp", "/projects/myapp")
	if got != "myapp-2" {
		t.Errorf("got %q, want %q", got, "myapp-2")
	}
}

func TestFreeSiteName_multipleCollisions(t *testing.T) {
	setupSitesYAML(t, `sites:
  - name: myapp
    domains:
      - myapp.test
    path: /projects/one
    php_version: "8.3"
    node_version: "22"
  - name: myapp-2
    domains:
      - myapp-2.test
    path: /projects/two
    php_version: "8.3"
    node_version: "22"
`)
	// Both "myapp" and "myapp-2" are taken → should get "myapp-3"
	got := freeSiteName("myapp", "/projects/three")
	if got != "myapp-3" {
		t.Errorf("got %q, want %q", got, "myapp-3")
	}
}

func TestFreeSiteName_legacyDomainField(t *testing.T) {
	// Legacy sites.yaml with "domain:" instead of "domains:" should still work
	setupSitesYAML(t, `sites:
  - name: myapp
    domain: myapp.test
    path: /projects/other
    php_version: "8.3"
    node_version: "22"
`)
	got := freeSiteName("myapp", "/projects/new")
	if got != "myapp-2" {
		t.Errorf("got %q, want %q", got, "myapp-2")
	}
}

// ── RegisterProject skips custom containers ─────────────────────────────────

func TestRegisterProject_SkipsCustomContainer(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	// Create a project directory with a composer.json so it looks like a PHP project.
	projectDir := filepath.Join(t.TempDir(), "nestapp")
	os.MkdirAll(projectDir, 0755)
	os.WriteFile(filepath.Join(projectDir, "composer.json"), []byte(`{}`), 0644)

	// Pre-register the site as a custom container.
	config.AddSite(config.Site{
		Name:          "nestapp",
		Domains:       []string{"nestapp.test"},
		Path:          projectDir,
		ContainerPort: 3000,
	})

	cfg := &config.GlobalConfig{}
	cfg.DNS.TLD = "test"
	cfg.PHP.DefaultVersion = "8.4"

	registered, err := RegisterProject(projectDir, cfg)
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	if registered {
		t.Error("RegisterProject should return false for a custom container site")
	}
}
