package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// writeWorktreeLerdYAML writes a minimal worktree .lerd.yaml at dir with the
// given db_isolated value.
func writeWorktreeLerdYAML(t *testing.T, dir string, isolated bool) {
	t.Helper()
	body := "workers:\n    - vite\n"
	if isolated {
		body += "db_isolated: true\n"
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureWorktreeIsolatedDB_NoopWhenNotOptedIn(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	wt := t.TempDir()
	writeWorktreeLerdYAML(t, wt, false) // no db_isolated

	site := &config.Site{Name: "parkapp"}
	created, err := EnsureWorktreeIsolatedDB(site, "feat/x", wt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("expected no provisioning when the worktree did not opt in")
	}
}

func TestEnsureWorktreeIsolatedDB_NoopWhenAlreadyRegistered(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	wt := t.TempDir()
	writeWorktreeLerdYAML(t, wt, true) // db_isolated: true

	// A registry entry already exists for this site+branch, so the helper must
	// short-circuit before attempting to create the DB (which would need podman).
	if err := config.AddWorktreeDB(config.WorktreeDBEntry{
		Site:    "parkapp",
		Branch:  "feat/x",
		Service: "postgres",
		DBName:  "parkapp_feat_x",
	}); err != nil {
		t.Fatal(err)
	}

	site := &config.Site{Name: "parkapp"}
	created, err := EnsureWorktreeIsolatedDB(site, "feat/x", wt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("expected no provisioning when the worktree DB is already registered")
	}
}
