package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeServiceFiles_OverwritesReadOnlyFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	svc := &CustomService{
		Name:  "pgadmin",
		Image: "docker.io/dpage/pgadmin4:latest",
		Files: []FileMount{{
			Target:  "/pgpass",
			Mode:    "0600",
			Chown:   true,
			Content: "host:5432:*:postgres:secret\n",
		}},
	}

	if err := MaterializeServiceFiles(svc); err != nil {
		t.Fatalf("first materialize: %v", err)
	}

	path := ServiceFilePath(svc.Name, "/pgpass")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("chmod 0400: %v", err)
	}

	svc.Files[0].Content = "host:5432:*:postgres:rotated\n"
	if err := MaterializeServiceFiles(svc); err != nil {
		t.Fatalf("rewrite over read-only file: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "host:5432:*:postgres:rotated\n" {
		t.Errorf("unexpected content: %q", string(got))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	if got, want := filepath.Dir(path), ServiceFilesDir(svc.Name); got != want {
		t.Errorf("dir = %q, want %q", got, want)
	}
}
