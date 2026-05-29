package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/siteops"
)

func TestNginxShow_printsSavedOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.NginxCustomD(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.NginxCustomD(), "acme.test.conf"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := siteops.ReadCustomNginx(resolveNginxTestDomain(t, "acme", ""))
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "# hi\n" || !got.Exists {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveNginxDomain_branchResolvesWorktreeDomain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	mainSite := filepath.Join(t.TempDir(), "acme")
	survivor := filepath.Join(t.TempDir(), "acme-feat")
	for _, d := range []string{filepath.Join(mainSite, ".git", "worktrees", "feat"), survivor} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	wtMeta := filepath.Join(mainSite, ".git", "worktrees", "feat")
	if err := os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(survivor, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: mainSite, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	_, domain, err := resolveNginxDomain([]string{"acme"}, "feat")
	if err != nil {
		t.Fatal(err)
	}
	if domain != "feat.acme.test" {
		t.Fatalf("branch domain = %q, want feat.acme.test", domain)
	}

	if _, _, err := resolveNginxDomain([]string{"acme"}, "nope"); err == nil {
		t.Fatal("expected error for unknown branch")
	}
}

func resolveNginxTestDomain(t *testing.T, name, branch string) string {
	t.Helper()
	_, domain, err := resolveNginxDomain([]string{name}, branch)
	if err != nil {
		t.Fatal(err)
	}
	return domain
}
