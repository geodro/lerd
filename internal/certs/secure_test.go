package certs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestWorktreeCertDomains_appendsWorktreeDomains(t *testing.T) {
	site := []string{"myapp.test"}
	wt := []string{"feat-x.myapp.test", "feat-y.myapp.test"}

	got := WorktreeCertDomains(site, wt)

	want := []string{"myapp.test", "feat-x.myapp.test", "feat-y.myapp.test"}
	if len(got) != len(want) {
		t.Fatalf("got %d domains, want %d: %v", len(got), len(want), got)
	}
	for i, d := range want {
		if got[i] != d {
			t.Errorf("domain[%d] = %q, want %q", i, got[i], d)
		}
	}
}

func TestWorktreeCertDomains_noWorktrees(t *testing.T) {
	got := WorktreeCertDomains([]string{"myapp.test"}, nil)
	if len(got) != 1 || got[0] != "myapp.test" {
		t.Errorf("expected only site domain, got: %v", got)
	}
}

func TestWorktreeCertDomains_doesNotMutateInput(t *testing.T) {
	site := []string{"myapp.test"}
	_ = WorktreeCertDomains(site, []string{"feat.myapp.test"})
	if len(site) != 1 {
		t.Error("WorktreeCertDomains mutated the input slice")
	}
}

// ReissueCertForWorktree must enumerate the site's worktrees and include
// each worktree subdomain (e.g. <branch>.<primary>) in the SAN list. This
// pins the contract that any domain mutation routed through this helper
// keeps existing worktree coverage; a regression where a handler swapped
// back to bare IssueCertForce would drop those SANs and break the
// worktree URLs in the browser.
func TestReissueCertForWorktree_includesWorktreeSubdomains(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Fake mkcert that echoes the SAN args into the cert so the test can
	// grep them. Matches the pattern used by manager_test.go.
	fakeMkcert := `#!/bin/sh
CRT=""
KEY=""
SANS=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -cert-file) shift; CRT="$1" ;;
    -key-file)  shift; KEY="$1" ;;
    *) SANS="$SANS $1" ;;
  esac
  shift
done
printf '%s' "$SANS" > "$CRT"
printf 'KEY' > "$KEY"
`
	if err := os.WriteFile(filepath.Join(binDir, "mkcert"), []byte(fakeMkcert), 0755); err != nil {
		t.Fatal(err)
	}

	// Lay out a fake main repo with one worktree on branch "main".
	sitePath := filepath.Join(tmp, "project")
	checkout := filepath.Join(tmp, "worktree-main")
	wtDir := filepath.Join(sitePath, ".git", "worktrees", "main")
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(checkout, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	site := config.Site{
		Name:    "harborlist",
		Path:    sitePath,
		Domains: []string{"harborlist.test", "aaaddd.test"},
		Secured: true,
	}

	if err := ReissueCertForWorktree(site); err != nil {
		t.Fatalf("ReissueCertForWorktree: %v", err)
	}

	certPath := filepath.Join(tmp, "lerd", "certs", "sites", "harborlist.test.crt")
	body, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("reading cert: %v", err)
	}
	got := string(body)
	wantSANs := []string{
		"harborlist.test",
		"*.harborlist.test",
		"aaaddd.test",
		"*.aaaddd.test",
		"main.harborlist.test",
		"*.main.harborlist.test",
	}
	for _, san := range wantSANs {
		if !strings.Contains(got, san) {
			t.Errorf("SAN %q missing from cert; got %q", san, got)
		}
	}
}

func TestSecureSite_RefusesWhenDNSDisabled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg := &config.GlobalConfig{}
	cfg.DNS.Enabled = false
	cfg.DNS.TLD = "localhost"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	site := config.Site{Name: "myapp", Domains: []string{"myapp.localhost"}}
	err := SecureSite(site)
	if !errors.Is(err, ErrDNSDisabled) {
		t.Fatalf("SecureSite err = %v, want ErrDNSDisabled", err)
	}
}
