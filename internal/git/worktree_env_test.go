package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// EnsureWorktreeEnv must materialise .env in a fresh worktree (git worktree
// add does not carry it across because the file is gitignored). The main
// repo's .env is the source; APP_URL is rewritten to the worktree domain.
func TestEnsureWorktreeEnv_copiesFromMainAndRewritesAppURL(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	mainEnv := "APP_NAME=acme\nAPP_URL=http://acme.test\nDB_HOST=mysql\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", false)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatalf("worktree .env not created: %v", err)
	}
	if !strings.Contains(string(got), "APP_URL=http://feat-a.acme.test") {
		t.Errorf("APP_URL not rewritten:\n%s", got)
	}
	if !strings.Contains(string(got), "DB_HOST=mysql") {
		t.Errorf(".env not copied in full:\n%s", got)
	}
}

// When the worktree already has its own .env, we keep it but realign APP_URL.
func TestEnsureWorktreeEnv_preservesExistingEnvAndRealignsURL(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	if err := os.WriteFile(filepath.Join(main, ".env"), []byte("APP_URL=http://main.test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	custom := "APP_URL=http://stale.test\nMY_KEY=keep-me\n"
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte(custom), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", true)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "APP_URL=https://feat-a.acme.test") {
		t.Errorf("APP_URL not realigned to https worktree:\n%s", got)
	}
	if !strings.Contains(string(got), "MY_KEY=keep-me") {
		t.Errorf("worktree-specific keys lost:\n%s", got)
	}
}

// When .lerd.yaml has env_overrides, templates are resolved and applied.
func TestEnsureWorktreeEnv_appliesEnvOverrides(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	mainEnv := "APP_URL=http://acme.test\nCENTRAL_DOMAIN=acme.test\nDB_HOST=mysql\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}
	lerdYAML := "domains:\n  - acme\nenv_overrides:\n  APP_URL: \"{{scheme}}://app.{{domain}}\"\n  CENTRAL_DOMAIN: \"{{domain}}\"\n"
	if err := os.WriteFile(filepath.Join(main, ".lerd.yaml"), []byte(lerdYAML), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", true)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatalf("worktree .env not created: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "APP_URL=https://app.feat-a.acme.test") {
		t.Errorf("APP_URL not resolved from override:\n%s", s)
	}
	if !strings.Contains(s, "CENTRAL_DOMAIN=feat-a.acme.test") {
		t.Errorf("CENTRAL_DOMAIN not resolved from override:\n%s", s)
	}
	if !strings.Contains(s, "DB_HOST=mysql") {
		t.Errorf("non-overridden keys should be preserved:\n%s", s)
	}
}

// env_overrides with {{site}} placeholder resolves to underscored domain.
func TestEnsureWorktreeEnv_siteTemplatePlaceholder(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	mainEnv := "APP_URL=http://acme.test\nDB_DATABASE=acme\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}
	lerdYAML := "domains:\n  - acme\nenv_overrides:\n  DB_DATABASE: \"{{site}}\"\n"
	if err := os.WriteFile(filepath.Join(main, ".lerd.yaml"), []byte(lerdYAML), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", false)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "DB_DATABASE=feat_a_acme_test") {
		t.Errorf("{{site}} not resolved:\n%s", got)
	}
}

// Static values (no placeholders) are written as-is.
func TestEnsureWorktreeEnv_staticOverrideValues(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	mainEnv := "APP_URL=http://acme.test\nCACHE_DRIVER=file\nQUEUE_CONNECTION=sync\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}
	lerdYAML := "domains:\n  - acme\nenv_overrides:\n  APP_URL: \"{{scheme}}://app.{{domain}}\"\n  CACHE_DRIVER: \"redis\"\n  NEW_KEY: \"static-value\"\n"
	if err := os.WriteFile(filepath.Join(main, ".lerd.yaml"), []byte(lerdYAML), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", true)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "APP_URL=https://app.feat-a.acme.test") {
		t.Errorf("templated override not applied:\n%s", s)
	}
	if !strings.Contains(s, "CACHE_DRIVER=redis") {
		t.Errorf("static override not applied:\n%s", s)
	}
	if !strings.Contains(s, "NEW_KEY=static-value") {
		t.Errorf("new static key not appended:\n%s", s)
	}
	if !strings.Contains(s, "QUEUE_CONNECTION=sync") {
		t.Errorf("non-overridden keys should be preserved:\n%s", s)
	}
}

// Without env_overrides in .lerd.yaml, falls back to default APP_URL rewrite.
func TestEnsureWorktreeEnv_fallsBackWithoutOverrides(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	mainEnv := "APP_URL=http://acme.test\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}
	lerdYAML := "domains:\n  - acme\n"
	if err := os.WriteFile(filepath.Join(main, ".lerd.yaml"), []byte(lerdYAML), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", true)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "APP_URL=https://feat-a.acme.test") {
		t.Errorf("should fall back to default APP_URL rewrite:\n%s", got)
	}
}

// No-op when the main repo has no .env (lerd should not invent one out of
// thin air; it simply has nothing to copy).
func TestEnsureWorktreeEnv_noopWhenMainHasNoEnv(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", false)

	if _, err := os.Stat(filepath.Join(wt, ".env")); !os.IsNotExist(err) {
		t.Errorf("expected no .env in worktree, got err=%v", err)
	}
}
