package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func writePhpConfig(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "wp-config.php")
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

// ── ReadPhpConst ──────────────────────────────────────────────────────────────

func TestReadPhpConst_SingleQuotes(t *testing.T) {
	f := writePhpConfig(t, `<?php
define('DB_NAME', 'mydb');
define('DB_USER', 'root');
`)
	got, err := ReadPhpConst(f)
	if err != nil {
		t.Fatalf("ReadPhpConst: %v", err)
	}
	if got["DB_NAME"] != "mydb" {
		t.Errorf("DB_NAME = %q, want %q", got["DB_NAME"], "mydb")
	}
	if got["DB_USER"] != "root" {
		t.Errorf("DB_USER = %q, want %q", got["DB_USER"], "root")
	}
}

func TestReadPhpConst_DoubleQuotes(t *testing.T) {
	f := writePhpConfig(t, `<?php
define("DB_HOST", "localhost");
define("DB_PASSWORD", "secret");
`)
	got, err := ReadPhpConst(f)
	if err != nil {
		t.Fatal(err)
	}
	if got["DB_HOST"] != "localhost" {
		t.Errorf("DB_HOST = %q, want localhost", got["DB_HOST"])
	}
	if got["DB_PASSWORD"] != "secret" {
		t.Errorf("DB_PASSWORD = %q, want secret", got["DB_PASSWORD"])
	}
}

func TestReadPhpConst_MixedQuotes(t *testing.T) {
	f := writePhpConfig(t, `<?php
define('DB_NAME', 'mydb');
define("DB_HOST", "localhost");
`)
	got, err := ReadPhpConst(f)
	if err != nil {
		t.Fatal(err)
	}
	if got["DB_NAME"] != "mydb" || got["DB_HOST"] != "localhost" {
		t.Errorf("mixed quotes not parsed correctly: %v", got)
	}
}

func TestReadPhpConst_WithWhitespace(t *testing.T) {
	f := writePhpConfig(t, `<?php
define( 'DB_NAME' , 'mydb' );
`)
	got, err := ReadPhpConst(f)
	if err != nil {
		t.Fatal(err)
	}
	if got["DB_NAME"] != "mydb" {
		t.Errorf("DB_NAME = %q, want mydb (whitespace inside define should be ok)", got["DB_NAME"])
	}
}

func TestReadPhpConst_MissingKey(t *testing.T) {
	f := writePhpConfig(t, `<?php
define('DB_NAME', 'mydb');
`)
	got, err := ReadPhpConst(f)
	if err != nil {
		t.Fatal(err)
	}
	if val, ok := got["DB_HOST"]; ok {
		t.Errorf("expected DB_HOST to be absent, got %q", val)
	}
}

func TestReadPhpConst_MissingFile(t *testing.T) {
	_, err := ReadPhpConst("/nonexistent/wp-config.php")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadPhpConst_EmptyFile(t *testing.T) {
	f := writePhpConfig(t, "")
	got, err := ReadPhpConst(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map for empty file, got %v", got)
	}
}

// ── ApplyPhpConstUpdates ──────────────────────────────────────────────────────

func TestApplyPhpConstUpdates_UpdatesExisting(t *testing.T) {
	f := writePhpConfig(t, `<?php
define('DB_NAME', 'olddb');
define('DB_HOST', 'localhost');
`)
	if err := ApplyPhpConstUpdates(f, map[string]string{"DB_NAME": "newdb"}); err != nil {
		t.Fatalf("ApplyPhpConstUpdates: %v", err)
	}
	got, _ := ReadPhpConst(f)
	if got["DB_NAME"] != "newdb" {
		t.Errorf("DB_NAME = %q, want newdb", got["DB_NAME"])
	}
	if got["DB_HOST"] != "localhost" {
		t.Error("DB_HOST should remain unchanged")
	}
}

func TestApplyPhpConstUpdates_AppendsBeforeThatsAll(t *testing.T) {
	content := `<?php
define('DB_NAME', 'mydb');

/* That's all, stop editing! Happy publishing. */
`
	f := writePhpConfig(t, content)
	if err := ApplyPhpConstUpdates(f, map[string]string{"NEW_KEY": "newval"}); err != nil {
		t.Fatalf("ApplyPhpConstUpdates: %v", err)
	}

	data, _ := os.ReadFile(f)
	s := string(data)
	// New key must appear before "That's all"
	newKeyPos := indexOf(s, "NEW_KEY")
	thatsAllPos := indexOf(s, "/* That's all")
	if newKeyPos == -1 {
		t.Error("NEW_KEY not found in file")
	} else if thatsAllPos != -1 && newKeyPos > thatsAllPos {
		t.Error("NEW_KEY should be inserted before \"/* That's all\"")
	}
}

func TestApplyPhpConstUpdates_MissingFile(t *testing.T) {
	err := ApplyPhpConstUpdates("/nonexistent/wp-config.php", map[string]string{"K": "v"})
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// indexOf returns the byte index of substr in s, or -1.
func indexOf(s, substr string) int {
	for i := range s {
		if i+len(substr) <= len(s) && s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
