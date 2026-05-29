package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/siteops"
)

func mcpText(t *testing.T, result any) string {
	t.Helper()
	content := result.(map[string]any)["content"].([]map[string]any)
	return content[0]["text"].(string)
}

func mcpIsError(result any) bool {
	_, has := result.(map[string]any)["isError"]
	return has
}

func stubNginxApply(t *testing.T) {
	t.Helper()
	prevR, prevT := siteops.NginxReloadFn, siteops.NginxTestFn
	siteops.NginxReloadFn = func() error { return nil }
	siteops.NginxTestFn = func() (string, error) { return "ok", nil }
	t.Cleanup(func() { siteops.NginxReloadFn, siteops.NginxTestFn = prevR, prevT })
}

func TestExecSiteNginx_readWriteReset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubNginxApply(t)
	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	// write
	res, _ := execSiteNginxWrite(map[string]any{"site": "acme", "content": "client_max_body_size 100m;\n"})
	if mcpIsError(res) {
		t.Fatalf("write errored: %s", mcpText(t, res))
	}
	onDisk, _ := os.ReadFile(filepath.Join(config.NginxCustomD(), "acme.test.conf"))
	if string(onDisk) != "client_max_body_size 100m;\n" {
		t.Fatalf("on-disk = %q", onDisk)
	}

	// read
	res, _ = execSiteNginxRead(map[string]any{"site": "acme"})
	if !strings.Contains(mcpText(t, res), "client_max_body_size 100m;") {
		t.Fatalf("read missing content: %s", mcpText(t, res))
	}

	// reset
	res, _ = execSiteNginxReset(map[string]any{"site": "acme"})
	if mcpIsError(res) {
		t.Fatalf("reset errored: %s", mcpText(t, res))
	}
	if _, err := os.Stat(filepath.Join(config.NginxCustomD(), "acme.test.conf")); !os.IsNotExist(err) {
		t.Fatal("override should be removed after reset")
	}
}

func TestExecSiteNginx_branchTargetsWorktree(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubNginxApply(t)

	mainSite := filepath.Join(t.TempDir(), "acme")
	survivor := filepath.Join(t.TempDir(), "acme-feat")
	for _, d := range []string{filepath.Join(mainSite, ".git", "worktrees", "feat"), survivor} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	wtMeta := filepath.Join(mainSite, ".git", "worktrees", "feat")
	_ = os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/feat\n"), 0o644)
	_ = os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(survivor, ".git")+"\n"), 0o644)
	if err := config.AddSite(config.Site{Name: "acme", Path: mainSite, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	res, _ := execSiteNginxWrite(map[string]any{"site": "acme", "branch": "feat", "content": "# wt\n"})
	if mcpIsError(res) {
		t.Fatalf("write errored: %s", mcpText(t, res))
	}
	if _, err := os.Stat(filepath.Join(config.NginxCustomD(), "feat.acme.test.conf")); err != nil {
		t.Fatalf("worktree override not written: %v", err)
	}

	if res, _ := execSiteNginxWrite(map[string]any{"site": "acme", "branch": "nope", "content": "x"}); !mcpIsError(res) {
		t.Fatal("expected error for unknown branch")
	}
}
