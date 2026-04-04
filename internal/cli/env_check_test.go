package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func envPaths(dir string, names ...string) (string, []string) {
	example := filepath.Join(dir, ".env.example")
	envs := make([]string, len(names))
	for i, n := range names {
		envs[i] = filepath.Join(dir, n)
	}
	return example, envs
}

func TestEnvCheck_inSync(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".env.example", "APP_NAME=\nDB_HOST=\n")
	writeFile(t, dir, ".env", "APP_NAME=MyApp\nDB_HOST=localhost\n")

	ex, envs := envPaths(dir, ".env")
	missing, extra := diffEnvKeys(ex, envs[0])
	if len(missing) != 0 {
		t.Errorf("expected no missing keys, got %v", missing)
	}
	if len(extra) != 0 {
		t.Errorf("expected no extra keys, got %v", extra)
	}
}

func TestEnvCheck_missingKeys(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".env.example", "APP_NAME=\nDB_HOST=\nMAIL_HOST=\n")
	writeFile(t, dir, ".env", "APP_NAME=MyApp\n")

	ex, envs := envPaths(dir, ".env")
	missing, extra := diffEnvKeys(ex, envs[0])
	if len(missing) != 2 {
		t.Errorf("expected 2 missing keys, got %v", missing)
	}
	if len(extra) != 0 {
		t.Errorf("expected no extra keys, got %v", extra)
	}
}

func TestEnvCheck_extraKeys(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".env.example", "APP_NAME=\n")
	writeFile(t, dir, ".env", "APP_NAME=MyApp\nCUSTOM_KEY=val\n")

	ex, envs := envPaths(dir, ".env")
	missing, extra := diffEnvKeys(ex, envs[0])
	if len(missing) != 0 {
		t.Errorf("expected no missing keys, got %v", missing)
	}
	if len(extra) != 1 || extra[0] != "CUSTOM_KEY" {
		t.Errorf("expected [CUSTOM_KEY], got %v", extra)
	}
}

func TestEnvCheck_commentsIgnored(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".env.example", "# comment\nAPP_NAME=\n")
	writeFile(t, dir, ".env", "# different comment\nAPP_NAME=MyApp\n")

	ex, envs := envPaths(dir, ".env")
	missing, extra := diffEnvKeys(ex, envs[0])
	if len(missing) != 0 {
		t.Errorf("expected no missing keys, got %v", missing)
	}
	if len(extra) != 0 {
		t.Errorf("expected no extra keys, got %v", extra)
	}
}

func TestFindEnvFiles_findsVariants(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".env", "A=1\n")
	writeFile(t, dir, ".env.testing", "A=1\n")
	writeFile(t, dir, ".env.example", "A=\n")
	writeFile(t, dir, ".env.local", "A=1\n")
	writeFile(t, dir, "unrelated.txt", "x\n")

	files := findEnvFiles(dir)
	// Should include .env, .env.testing, .env.local but not .env.example
	found := map[string]bool{}
	for _, f := range files {
		found[filepath.Base(f)] = true
	}
	if !found[".env"] {
		t.Error("expected .env")
	}
	if !found[".env.testing"] {
		t.Error("expected .env.testing")
	}
	if !found[".env.local"] {
		t.Error("expected .env.local")
	}
	if found[".env.example"] {
		t.Error("should not include .env.example")
	}
	if found["unrelated.txt"] {
		t.Error("should not include unrelated.txt")
	}
}
