package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// alignWorktreeEnvDBConnection mirrors the parent's DB connection coordinates
// into each worktree .env (the worktree arm of `lerd env`), while leaving the
// worktree-specific DB_DATABASE alone.
func TestAlignWorktreeEnvDBConnection_realignsHostKeepsDatabase(t *testing.T) {
	main := t.TempDir()
	checkout := t.TempDir()

	// Main repo .git dir so IsMainRepo is true, plus the worktree metadata that
	// DetectWorktrees reads (HEAD for the branch, gitdir for the checkout path).
	wtMeta := filepath.Join(main, ".git", "worktrees", "feat")
	if err := os.MkdirAll(wtMeta, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/feat/add-social-logins\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Parent .env already aligned to the current service; worktree .env is stale.
	mainEnv := "DB_CONNECTION=pgsql\nDB_HOST=lerd-postgres-18\nDB_PORT=5432\nDB_DATABASE=acme\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}
	wtEnv := "DB_CONNECTION=pgsql\nDB_HOST=lerd-postgres\nDB_PORT=5432\nDB_DATABASE=acme_feat_add_social_logins\n"
	if err := os.WriteFile(filepath.Join(checkout, ".env"), []byte(wtEnv), 0644); err != nil {
		t.Fatal(err)
	}

	site := &config.Site{Name: "acme", Path: main, Domains: []string{"acme.test"}}
	alignWorktreeEnvDBConnection(site, filepath.Join(main, ".env"), ".env", "")

	got, err := os.ReadFile(filepath.Join(checkout, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "DB_HOST=lerd-postgres-18") {
		t.Errorf("worktree DB_HOST not realigned to parent:\n%s", s)
	}
	if strings.Contains(s, "DB_HOST=lerd-postgres\n") {
		t.Errorf("stale worktree DB_HOST still present:\n%s", s)
	}
	if !strings.Contains(s, "DB_DATABASE=acme_feat_add_social_logins") {
		t.Errorf("worktree-specific DB_DATABASE must be left untouched:\n%s", s)
	}
}

// A worktree that has no .env yet is skipped without error.
func TestAlignWorktreeEnvDBConnection_skipsWorktreeWithoutEnv(t *testing.T) {
	main := t.TempDir()
	checkout := t.TempDir()

	wtMeta := filepath.Join(main, ".git", "worktrees", "feat")
	if err := os.MkdirAll(wtMeta, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/feat\n"), 0644)
	os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0644)
	os.WriteFile(filepath.Join(main, ".env"), []byte("DB_HOST=lerd-postgres-18\n"), 0644)

	site := &config.Site{Name: "acme", Path: main, Domains: []string{"acme.test"}}
	// No worktree .env: must be a no-op, no panic, no file created.
	alignWorktreeEnvDBConnection(site, filepath.Join(main, ".env"), ".env", "")

	if _, err := os.Stat(filepath.Join(checkout, ".env")); !os.IsNotExist(err) {
		t.Error("worktree .env should not have been created")
	}
}
