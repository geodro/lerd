package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnv(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

func readEnv(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// ── ApplyUpdates ─────────────────────────────────────────────────────────────

func TestApplyUpdates_replacesExistingKey(t *testing.T) {
	f := writeEnv(t, "APP_NAME=MyApp\nAPP_URL=http://old.test\nAPP_ENV=local\n")
	if err := ApplyUpdates(f, map[string]string{"APP_URL": "https://new.test"}); err != nil {
		t.Fatal(err)
	}
	got := readEnv(t, f)
	if !strings.Contains(got, "APP_URL=https://new.test") {
		t.Errorf("expected new APP_URL, got:\n%s", got)
	}
	if strings.Contains(got, "http://old.test") {
		t.Error("old value should be gone")
	}
}

func TestApplyUpdates_appendsMissingKey(t *testing.T) {
	f := writeEnv(t, "APP_NAME=MyApp\n")
	if err := ApplyUpdates(f, map[string]string{"APP_URL": "http://myapp.test"}); err != nil {
		t.Fatal(err)
	}
	got := readEnv(t, f)
	if !strings.Contains(got, "APP_URL=http://myapp.test") {
		t.Errorf("expected APP_URL to be appended, got:\n%s", got)
	}
	if !strings.Contains(got, "APP_NAME=MyApp") {
		t.Error("existing keys should be preserved")
	}
}

func TestApplyUpdates_preservesCommentsAndBlanks(t *testing.T) {
	f := writeEnv(t, "# App settings\nAPP_NAME=MyApp\n\n# DB\nDB_HOST=localhost\n")
	if err := ApplyUpdates(f, map[string]string{"DB_HOST": "db.internal"}); err != nil {
		t.Fatal(err)
	}
	got := readEnv(t, f)
	if !strings.Contains(got, "# App settings") {
		t.Error("comments should be preserved")
	}
	if !strings.Contains(got, "APP_NAME=MyApp") {
		t.Error("unrelated keys should be preserved")
	}
	if !strings.Contains(got, "DB_HOST=db.internal") {
		t.Error("updated key missing")
	}
}

func TestApplyUpdates_multipleUpdates(t *testing.T) {
	f := writeEnv(t, "APP_URL=http://old.test\nDB_HOST=localhost\nAPP_ENV=local\n")
	if err := ApplyUpdates(f, map[string]string{
		"APP_URL": "https://new.test",
		"DB_HOST": "db.prod",
	}); err != nil {
		t.Fatal(err)
	}
	got := readEnv(t, f)
	if !strings.Contains(got, "APP_URL=https://new.test") {
		t.Errorf("APP_URL not updated in:\n%s", got)
	}
	if !strings.Contains(got, "DB_HOST=db.prod") {
		t.Errorf("DB_HOST not updated in:\n%s", got)
	}
	if !strings.Contains(got, "APP_ENV=local") {
		t.Error("unrelated key should be preserved")
	}
}

func TestApplyUpdates_missingFile(t *testing.T) {
	err := ApplyUpdates("/nonexistent/.env", map[string]string{"K": "v"})
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestApplyUpdates_emptyUpdates(t *testing.T) {
	content := "APP_NAME=MyApp\n"
	f := writeEnv(t, content)
	if err := ApplyUpdates(f, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	got := readEnv(t, f)
	if !strings.Contains(got, "APP_NAME=MyApp") {
		t.Error("file should be unchanged with empty updates")
	}
}

func TestApplyUpdates_skipsCommentedKeys(t *testing.T) {
	// A commented-out APP_URL should not be treated as a value to replace
	f := writeEnv(t, "# APP_URL=http://commented.test\nAPP_URL=http://real.test\n")
	if err := ApplyUpdates(f, map[string]string{"APP_URL": "https://new.test"}); err != nil {
		t.Fatal(err)
	}
	got := readEnv(t, f)
	if strings.Contains(got, "http://real.test") {
		t.Error("real APP_URL should have been replaced")
	}
	if !strings.Contains(got, "APP_URL=https://new.test") {
		t.Error("new APP_URL missing")
	}
	// Comment line should remain untouched
	if !strings.Contains(got, "# APP_URL=http://commented.test") {
		t.Error("comment line should be preserved as-is")
	}
}

func TestApplyUpdates_uncomments(t *testing.T) {
	f := writeEnv(t, "APP_NAME=MyApp\n# DB_HOST=127.0.0.1\n# DB_PORT=3306\nDB_DATABASE=laravel\n")
	if err := ApplyUpdates(f, map[string]string{
		"DB_HOST": "mysql.internal",
		"DB_PORT": "3307",
	}); err != nil {
		t.Fatal(err)
	}
	got := readEnv(t, f)
	if !strings.Contains(got, "DB_HOST=mysql.internal") {
		t.Errorf("commented DB_HOST should be uncommented and updated, got:\n%s", got)
	}
	if !strings.Contains(got, "DB_PORT=3307") {
		t.Errorf("commented DB_PORT should be uncommented and updated, got:\n%s", got)
	}
	// Should be in place, not appended at the end
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 4 {
		t.Errorf("expected 4 lines (no appended duplicates), got %d:\n%s", len(lines), got)
	}
	if !strings.Contains(got, "APP_NAME=MyApp") {
		t.Error("existing keys should be preserved")
	}
	if !strings.Contains(got, "DB_DATABASE=laravel") {
		t.Error("existing keys should be preserved")
	}
}

// ── UpdateAppURL ──────────────────────────────────────────────────────────────

func TestUpdateAppURL_setsHTTPS(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_URL=http://old.test\n"), 0644)
	if err := UpdateAppURL(dir, "https", "myapp.test"); err != nil {
		t.Fatal(err)
	}
	got := readEnv(t, filepath.Join(dir, ".env"))
	if !strings.Contains(got, "APP_URL=https://myapp.test") {
		t.Errorf("expected https URL, got:\n%s", got)
	}
}

// ── ReadKeys ─────────────────────────────────────────────────────────────────

func TestReadKeys_returnsAllKeys(t *testing.T) {
	f := writeEnv(t, "APP_NAME=MyApp\nDB_HOST=localhost\nAPP_ENV=local\n")
	keys, err := ReadKeys(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"APP_NAME", "DB_HOST", "APP_ENV"}
	if len(keys) != len(want) {
		t.Fatalf("got %d keys, want %d", len(keys), len(want))
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("key[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestReadKeys_skipsCommentsAndBlanks(t *testing.T) {
	f := writeEnv(t, "# a comment\nAPP_NAME=MyApp\n\n# another\nDB_HOST=localhost\n")
	keys, err := ReadKeys(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2: %v", len(keys), keys)
	}
	if keys[0] != "APP_NAME" || keys[1] != "DB_HOST" {
		t.Errorf("unexpected keys: %v", keys)
	}
}

func TestReadKeys_missingFile(t *testing.T) {
	_, err := ReadKeys("/nonexistent/.env")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestUpdateAppURL_noEnvFile_silent(t *testing.T) {
	// Should silently return nil when .env doesn't exist
	err := UpdateAppURL(t.TempDir(), "https", "myapp.test")
	if err != nil {
		t.Errorf("expected no error for missing .env, got: %v", err)
	}
}
