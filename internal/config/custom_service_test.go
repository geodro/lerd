package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveCustomService_RejectsNewlineInEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	svc := &CustomService{
		Name:  "evil",
		Image: "alpine",
		Environment: map[string]string{
			"PAYLOAD": "ok\nExec=/bin/sh -c pwned",
		},
	}
	err := SaveCustomService(svc)
	if err == nil || !strings.Contains(err.Error(), "newline") {
		t.Fatalf("expected newline rejection, got: %v", err)
	}
}

func TestLoadCustomServiceFromFile_StripsLegacyFilesField(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "phpmyadmin.yaml")
	// Legacy YAML with a files: block (from older lerd versions, or a
	// malicious tamper). Must be stripped on load and the file re-saved
	// without it. The authoritative source is presetFiles in Go.
	yaml := `name: phpmyadmin
image: docker.io/library/phpmyadmin:latest
preset: phpmyadmin
files:
  - target: /etc/hosts
    content: "0.0.0.0 example.com"
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	svc, err := LoadCustomServiceFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(svc.Files) != 0 {
		t.Errorf("svc.Files should be empty after load, got %+v", svc.Files)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated: %v", err)
	}
	if strings.Contains(string(migrated), "files:") || strings.Contains(string(migrated), "/etc/hosts") {
		t.Errorf("legacy files: entry survived on-disk migration:\n%s", migrated)
	}
}

func TestMaterializeServiceFiles_OverwritesReadOnlyFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	// Use the real pgadmin preset so the materialiser picks up the
	// hardcoded file definitions (including the chown-required /pgpass).
	svc := &CustomService{
		Name:   "pgadmin",
		Image:  "docker.io/dpage/pgadmin4:latest",
		Preset: "pgadmin",
	}

	if err := MaterializeServiceFiles(svc); err != nil {
		t.Fatalf("first materialize: %v", err)
	}

	path := ServiceFilePath(svc.Name, "/pgpass")
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("chmod 0400: %v", err)
	}

	// Re-materialise: MaterializeServiceFiles must be able to overwrite the
	// read-only file (which is why it unlinks first).
	if err := MaterializeServiceFiles(svc); err != nil {
		t.Fatalf("rewrite over read-only file: %v", err)
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
