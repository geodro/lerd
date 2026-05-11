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

// ── SyncPrimaryDomain ────────────────────────────────────────────────────────

func TestSyncPrimaryDomain_updatesAllReverbVars(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte(
		"APP_URL=https://old.test\n"+
			"VITE_REVERB_HOST=old.test\n"+
			"VITE_REVERB_SCHEME=https\n"+
			"VITE_REVERB_PORT=443\n",
	), 0644)

	if err := SyncPrimaryDomain(dir, "new.test", false); err != nil {
		t.Fatal(err)
	}
	got := readEnv(t, filepath.Join(dir, ".env"))

	if !strings.Contains(got, "APP_URL=http://new.test") {
		t.Errorf("APP_URL not updated:\n%s", got)
	}
	if !strings.Contains(got, "VITE_REVERB_HOST=new.test") {
		t.Errorf("VITE_REVERB_HOST not updated:\n%s", got)
	}
	if !strings.Contains(got, "VITE_REVERB_SCHEME=http") {
		t.Errorf("VITE_REVERB_SCHEME not updated:\n%s", got)
	}
	if !strings.Contains(got, "VITE_REVERB_PORT=80") {
		t.Errorf("VITE_REVERB_PORT not updated:\n%s", got)
	}
}

func TestSyncPrimaryDomain_skipsAbsentKeys(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte(
		"APP_URL=http://old.test\nAPP_NAME=MyApp\n",
	), 0644)

	if err := SyncPrimaryDomain(dir, "new.test", true); err != nil {
		t.Fatal(err)
	}
	got := readEnv(t, filepath.Join(dir, ".env"))

	if !strings.Contains(got, "APP_URL=https://new.test") {
		t.Errorf("APP_URL not updated:\n%s", got)
	}
	// VITE_REVERB_HOST should NOT be added
	if strings.Contains(got, "VITE_REVERB_HOST") {
		t.Errorf("VITE_REVERB_HOST should not be added when absent:\n%s", got)
	}
}

func TestSyncPrimaryDomain_noEnvFile_silent(t *testing.T) {
	err := SyncPrimaryDomain(t.TempDir(), "new.test", true)
	if err != nil {
		t.Errorf("expected no error for missing .env, got: %v", err)
	}
}

// TestApplyUpdates_rejectsNewlineInValue pins the fix for the env-overrides
// injection vector: a value containing \n could split a single .env line
// into two, silently introducing an unrelated key. Refuse the write so the
// caller surfaces a clean error instead of mutating .env in place.
func TestApplyUpdates_rejectsNewlineInValue(t *testing.T) {
	f := writeEnv(t, "APP_NAME=MyApp\n")
	err := ApplyUpdates(f, map[string]string{"APP_URL": "http://x.test\nADMIN_TOKEN=stolen"})
	if err == nil {
		t.Fatal("expected error for value containing newline, got nil")
	}
	if !strings.Contains(err.Error(), "newline") && !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error should mention newline / invalid, got %v", err)
	}
	// .env must remain untouched.
	got := readEnv(t, f)
	if got != "APP_NAME=MyApp\n" {
		t.Errorf(".env was mutated despite invalid input; got:\n%s", got)
	}
}

// TestApplyUpdates_rejectsCarriageReturnInValue covers the same injection
// surface using \r alone (some Windows tooling produces CR-only values).
func TestApplyUpdates_rejectsCarriageReturnInValue(t *testing.T) {
	f := writeEnv(t, "APP_NAME=MyApp\n")
	err := ApplyUpdates(f, map[string]string{"FOO": "bar\rBAZ=evil"})
	if err == nil {
		t.Fatal("expected error for value containing CR, got nil")
	}
}

// TestApplyUpdates_rejectsNewlineInKey defends the same surface against
// key-side injection. ApplyUpdates also has to reject a literal '=' in the
// key, otherwise the resulting line still parses as the wrong key.
func TestApplyUpdates_rejectsNewlineInKey(t *testing.T) {
	f := writeEnv(t, "APP_NAME=MyApp\n")
	err := ApplyUpdates(f, map[string]string{"K1\nK2": "v"})
	if err == nil {
		t.Fatal("expected error for key containing newline, got nil")
	}
}

// TestApplyUpdates_rejectsEqualsInKey ensures keys with '=' don't slip
// through and corrupt the .env structure.
func TestApplyUpdates_rejectsEqualsInKey(t *testing.T) {
	f := writeEnv(t, "APP_NAME=MyApp\n")
	err := ApplyUpdates(f, map[string]string{"K=hack": "v"})
	if err == nil {
		t.Fatal("expected error for key containing =, got nil")
	}
}

// TestApplyUpdates_deterministicAppendOrder pins the fix for the map-range
// nondeterminism: two runs with identical inputs against an empty .env
// must produce identical bytes. Pre-fix the loop ranged over a Go map, so
// the first write of N new keys produced different byte orderings each
// run, defeating the "skip if unchanged" mtime guard on subsequent calls.
func TestApplyUpdates_deterministicAppendOrder(t *testing.T) {
	updates := map[string]string{
		"ZZZ": "1",
		"AAA": "2",
		"MMM": "3",
		"BBB": "4",
		"YYY": "5",
		"NNN": "6",
	}
	first := writeEnv(t, "APP_NAME=MyApp\n")
	if err := ApplyUpdates(first, updates); err != nil {
		t.Fatal(err)
	}
	firstOut := readEnv(t, first)

	for i := 0; i < 10; i++ {
		again := writeEnv(t, "APP_NAME=MyApp\n")
		if err := ApplyUpdates(again, updates); err != nil {
			t.Fatal(err)
		}
		againOut := readEnv(t, again)
		if firstOut != againOut {
			t.Fatalf("non-deterministic output on iteration %d:\nfirst:\n%s\nagain:\n%s", i, firstOut, againOut)
		}
	}
}
