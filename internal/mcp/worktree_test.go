package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestToolJSON_wrapsValueInContent(t *testing.T) {
	result := toolJSON(map[string]any{"site": "demo", "worktrees": []string{"a"}})
	if _, has := result["isError"]; has {
		t.Error("toolJSON must not set isError on success")
	}
	var parsed map[string]any
	decodeContent(t, result, &parsed)
	if parsed["site"] != "demo" {
		t.Errorf("expected site=demo, got %v", parsed["site"])
	}
}

// TestExecWorktreeList_returnsContent guards the regression where a handler
// returned a bare map with no "content" key, so the MCP host rendered a live
// worktree as "no output". The result must carry a content block.
func TestExecWorktreeList_returnsContent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))

	repo := filepath.Join(root, "demo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "README.md")
	gitRun(t, repo, "commit", "-m", "init")
	gitRun(t, repo, "worktree", "add", filepath.Join(repo, "feature"), "-b", "feature")

	if err := config.AddSite(config.Site{Name: "demo", Domains: []string{"demo.test"}, Path: repo}); err != nil {
		t.Fatal("add site:", err)
	}

	result, rpcErr := execWorktreeList(map[string]any{"site": "demo"})
	if rpcErr != nil {
		t.Fatal("unexpected rpc error:", rpcErr.Message)
	}
	var parsed struct {
		Site      string `json:"site"`
		Worktrees []struct {
			Branch string `json:"branch"`
			Domain string `json:"domain"`
		} `json:"worktrees"`
	}
	decodeContent(t, result, &parsed)
	if parsed.Site != "demo" {
		t.Errorf("expected site=demo, got %q", parsed.Site)
	}
	if len(parsed.Worktrees) != 1 || parsed.Worktrees[0].Branch != "feature" {
		t.Fatalf("expected one worktree on branch feature, got %+v", parsed.Worktrees)
	}
	if parsed.Worktrees[0].Domain != "feature.demo.test" {
		t.Errorf("expected domain feature.demo.test, got %q", parsed.Worktrees[0].Domain)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
