package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadEnvOverride_MissingFile(t *testing.T) {
	dir := t.TempDir()
	overrides, external := readEnvOverride(dir)
	if len(overrides) != 0 {
		t.Errorf("expected no overrides, got %v", overrides)
	}
	if len(external) != 0 {
		t.Errorf("expected no external services, got %v", external)
	}
}

func TestReadEnvOverride_ParsesValuesAndStripsReservedKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, envOverrideFile, `# personal overrides
DB_USERNAME=postgres
DB_PASSWORD="se cret"
LERD_EXTERNAL_SERVICES = postgres, Redis  mysql
`)

	overrides, external := readEnvOverride(dir)

	if overrides["DB_USERNAME"] != "postgres" {
		t.Errorf("DB_USERNAME = %q, want postgres", overrides["DB_USERNAME"])
	}
	// Quotes are preserved verbatim so the value round-trips into .env correctly.
	if overrides["DB_PASSWORD"] != `"se cret"` {
		t.Errorf("DB_PASSWORD = %q, want '\"se cret\"' (quotes preserved)", overrides["DB_PASSWORD"])
	}
	if _, leaked := overrides[envOverrideExternalKey]; leaked {
		t.Error("reserved key leaked into overrides map; it must never reach .env")
	}
	for _, want := range []string{"postgres", "redis", "mysql"} {
		if !external[want] {
			t.Errorf("expected %q in external set (comma/space split, lowercased); got %v", want, external)
		}
	}
}

func TestApplyEnvOverrideFile_CreatesTemplateAndGitignores(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "/vendor\n")

	created, err := applyEnvOverrideFile(dir, nil)
	if err != nil {
		t.Fatalf("applyEnvOverrideFile: %v", err)
	}
	if !created {
		t.Error("expected created=true on first run")
	}

	body, err := os.ReadFile(filepath.Join(dir, envOverrideFile))
	if err != nil {
		t.Fatalf("override file not created: %v", err)
	}
	if !strings.Contains(string(body), envOverrideExternalKey) {
		t.Error("template should document the reserved external-services key")
	}

	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), envOverrideFile) {
		t.Errorf(".gitignore should list %s; got:\n%s", envOverrideFile, gi)
	}

	// Second run is not a create, and must not duplicate the gitignore entry.
	created, err = applyEnvOverrideFile(dir, nil)
	if err != nil {
		t.Fatalf("second applyEnvOverrideFile: %v", err)
	}
	if created {
		t.Error("expected created=false when file already exists")
	}
	gi2, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(gi2), envOverrideFile) != 1 {
		t.Errorf("gitignore entry duplicated:\n%s", gi2)
	}
}

func TestApplyEnvOverrideFile_CreatesGitignoreWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	if _, err := applyEnvOverrideFile(dir, nil); err != nil {
		t.Fatalf("applyEnvOverrideFile: %v", err)
	}
	gi, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf(".gitignore not created: %v", err)
	}
	if strings.TrimSpace(string(gi)) != envOverrideFile {
		t.Errorf("new .gitignore = %q, want %q", strings.TrimSpace(string(gi)), envOverrideFile)
	}
}

func TestApplyEnvOverrideFile_SeedsKeyValueArgs(t *testing.T) {
	dir := t.TempDir()
	if _, err := applyEnvOverrideFile(dir, []string{"DB_USERNAME=postgres", "DB_PASSWORD=secret"}); err != nil {
		t.Fatalf("applyEnvOverrideFile: %v", err)
	}
	overrides, _ := readEnvOverride(dir)
	if overrides["DB_USERNAME"] != "postgres" || overrides["DB_PASSWORD"] != "secret" {
		t.Errorf("seeded args not readable back: %v", overrides)
	}
}

func TestApplyEnvOverrideFile_RejectsMalformedArg(t *testing.T) {
	dir := t.TempDir()
	if _, err := applyEnvOverrideFile(dir, []string{"NOTAPAIR"}); err == nil {
		t.Error("expected error for arg without '='")
	}
}

func TestExternalDBPicked(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]bool
		want bool
	}{
		{"builtin postgres", map[string]bool{"postgres": true}, true},
		{"builtin mysql", map[string]bool{"mysql": true}, true},
		{"non-db service", map[string]bool{"mailpit": true}, false},
		{"empty", map[string]bool{}, false},
	}
	for _, c := range cases {
		if got := externalDBPicked(c.set); got != c.want {
			t.Errorf("%s: externalDBPicked = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOverrideOr(t *testing.T) {
	base := map[string]string{"APP_KEY": "base", "OTHER": "x"}
	overrides := map[string]string{"APP_KEY": "from-override"}
	if got := overrideOr(overrides, base, "APP_KEY"); got != "from-override" {
		t.Errorf("override should win: got %q", got)
	}
	if got := overrideOr(overrides, base, "OTHER"); got != "x" {
		t.Errorf("base value when no override: got %q", got)
	}
	if got := overrideOr(overrides, base, "MISSING"); got != "" {
		t.Errorf("absent key yields empty: got %q", got)
	}
}
