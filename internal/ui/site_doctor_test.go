package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnv(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckAppKey(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// Missing key → fail with a key:generate fix.
	writeEnv(t, dir, ".env", "APP_NAME=Acme\nAPP_KEY=\n")
	if c := checkAppKey(envPath); c.Status != doctorFail || c.Fix != "key:generate" {
		t.Errorf("empty APP_KEY: got status=%q fix=%q, want fail/key:generate", c.Status, c.Fix)
	}

	// Set key → ok, no fix.
	writeEnv(t, dir, ".env", "APP_KEY=base64:abcdef==\n")
	if c := checkAppKey(envPath); c.Status != doctorOK || c.Fix != "" {
		t.Errorf("set APP_KEY: got status=%q fix=%q, want ok/none", c.Status, c.Fix)
	}
}

func TestCheckEnvDrift(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// No .env.example → not applicable.
	writeEnv(t, dir, ".env", "APP_KEY=x\n")
	if _, ok := checkEnvDrift(dir, envPath); ok {
		t.Error("expected drift check skipped when no .env.example")
	}

	// Example declares two keys the .env lacks → warn listing both.
	writeEnv(t, dir, ".env.example", "APP_KEY=\nNEW_ONE=\nNEW_TWO=\n")
	writeEnv(t, dir, ".env", "APP_KEY=x\n")
	c, ok := checkEnvDrift(dir, envPath)
	if !ok || c.Status != doctorWarn {
		t.Fatalf("missing keys: got ok=%v status=%q, want true/warn", ok, c.Status)
	}
	if !strings.Contains(c.Detail, "NEW_ONE") || !strings.Contains(c.Detail, "NEW_TWO") {
		t.Errorf("detail should name the missing keys, got %q", c.Detail)
	}

	// All example keys present → ok.
	writeEnv(t, dir, ".env", "APP_KEY=x\nNEW_ONE=1\nNEW_TWO=2\n")
	if c, ok := checkEnvDrift(dir, envPath); !ok || c.Status != doctorOK {
		t.Errorf("aligned env: got ok=%v status=%q, want true/ok", ok, c.Status)
	}
}

func TestCheckAppDebug(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// production + debug on → warn (the footgun).
	writeEnv(t, dir, ".env", "APP_ENV=production\nAPP_DEBUG=true\n")
	if c := checkAppDebug(envPath); c.Status != doctorWarn {
		t.Errorf("prod+debug: got %q, want warn", c.Status)
	}

	// local + debug on → ok (normal dev).
	writeEnv(t, dir, ".env", "APP_ENV=local\nAPP_DEBUG=true\n")
	if c := checkAppDebug(envPath); c.Status != doctorOK {
		t.Errorf("local+debug: got %q, want ok", c.Status)
	}

	// production + debug off → ok.
	writeEnv(t, dir, ".env", "APP_ENV=production\nAPP_DEBUG=false\n")
	if c := checkAppDebug(envPath); c.Status != doctorOK {
		t.Errorf("prod+nodebug: got %q, want ok", c.Status)
	}
}

func TestCheckStorageLink(t *testing.T) {
	// No public disk → not applicable.
	bare := t.TempDir()
	if _, ok := checkStorageLink(bare); ok {
		t.Error("expected skip when there's no storage/app/public")
	}

	// Uses public disk, public/ exists, symlink missing → warn + fix.
	missing := t.TempDir()
	mustMkdir(t, filepath.Join(missing, "storage", "app", "public"))
	mustMkdir(t, filepath.Join(missing, "public"))
	c, ok := checkStorageLink(missing)
	if !ok || c.Status != doctorWarn || c.Fix != "storage:link" {
		t.Errorf("missing link: got ok=%v status=%q fix=%q, want true/warn/storage:link", ok, c.Status, c.Fix)
	}

	// Symlink present → ok regardless of disk layout.
	linked := t.TempDir()
	mustMkdir(t, filepath.Join(linked, "public"))
	mustMkdir(t, filepath.Join(linked, "storage", "app", "public"))
	if err := os.Symlink("../storage/app/public", filepath.Join(linked, "public", "storage")); err != nil {
		t.Fatal(err)
	}
	if c, ok := checkStorageLink(linked); !ok || c.Status != doctorOK {
		t.Errorf("present link: got ok=%v status=%q, want true/ok", ok, c.Status)
	}
}

func TestMigrationsPending(t *testing.T) {
	pending := "  Migration name ....................... Batch / Status\n" +
		"  2014_10_12_000000_create_users_table .. [1] Ran\n" +
		"  2024_01_01_000000_create_orders_table . Pending\n"
	if !migrationsPending(pending) {
		t.Error("expected pending=true when a row is Pending")
	}

	allRan := "  2014_10_12_000000_create_users_table .. [1] Ran\n" +
		"  2019_08_19_000000_create_failed_jobs .. [1] Ran\n"
	if migrationsPending(allRan) {
		t.Error("expected pending=false when every row Ran")
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
