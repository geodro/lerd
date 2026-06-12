package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
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

// TestEnsureWorktreeEnv_branchAndParentTokens pins the new explicit
// templating tokens that disambiguate the surprising {{site}} semantics:
//
//   - {{branch}}: the worktree branch slug, no parent context. Lets users
//     write DB_DATABASE=app_{{branch}} and get app_feat_a (sane).
//   - {{parent}}: the parent site name, slugified. Lets users write
//     DB_PREFIX={{parent}}_ and get acme_ (matches their mental model
//     of "this is project acme").
//
// {{site}} is intentionally left alone for backward compatibility — it
// continues to resolve to the worktree FQDN slug (feat_a_acme_test).
// Documented as such; new templates should prefer {{parent}}/{{branch}}.
func TestEnsureWorktreeEnv_branchAndParentTokens(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	main := filepath.Join(tmp, "acme")
	if err := os.MkdirAll(main, 0755); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()

	if err := config.AddSite(config.Site{
		Name: "acme", Path: main, Domains: []string{"acme.test"},
	}); err != nil {
		t.Fatal(err)
	}

	mainEnv := "APP_URL=http://acme.test\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}
	lerdYAML := `domains:
  - acme
env_overrides:
  DB_BRANCH: "{{branch}}"
  DB_PARENT: "{{parent}}"
  DB_NAME: "{{parent}}_{{branch}}"
`
	if err := os.WriteFile(filepath.Join(main, ".lerd.yaml"), []byte(lerdYAML), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", false)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatalf("worktree .env not created: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "DB_BRANCH=feat-a") {
		t.Errorf("{{branch}} should resolve to the worktree branch slug; got:\n%s", s)
	}
	if !strings.Contains(s, "DB_PARENT=acme") {
		t.Errorf("{{parent}} should resolve to the parent site primary subdomain slug; got:\n%s", s)
	}
	if !strings.Contains(s, "DB_NAME=acme_feat-a") {
		t.Errorf("composite {{parent}}_{{branch}} should resolve; got:\n%s", s)
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

// env_overrides should only override the keys it declares. APP_URL must still
// get the default scheme://worktreeDomain rewrite when the user only overrides
// some other key (e.g. SESSION_DOMAIN).
func TestEnsureWorktreeEnv_partialOverridesStillRewriteAppURL(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	mainEnv := "APP_URL=http://acme.test\nSESSION_DOMAIN=acme.test\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}
	lerdYAML := "domains:\n  - acme\nenv_overrides:\n  SESSION_DOMAIN: \"{{domain}}\"\n"
	if err := os.WriteFile(filepath.Join(main, ".lerd.yaml"), []byte(lerdYAML), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-a.acme.test", true)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "APP_URL=https://feat-a.acme.test") {
		t.Errorf("APP_URL must still be rewritten when env_overrides omits it:\n%s", s)
	}
	if !strings.Contains(s, "SESSION_DOMAIN=feat-a.acme.test") {
		t.Errorf("declared override not applied:\n%s", s)
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

// TestEnsureWorktreeEnv_isolatedDBOverrideSkipped pins the isolation /
// env_overrides conflict resolution. When the user opts into an isolated
// worktree DB (lerd db:isolate writes a per-branch DB_DATABASE into the
// worktree's .env and sets db_isolated:true in its .lerd.yaml), subsequent
// EnsureWorktreeEnv ticks must NOT clobber DB_DATABASE from a parent
// env_overrides template, or the isolated DB silently goes back to the
// templated value on the next watcher refresh.
//
// Modelled on the real harborlist.test fixture (Laravel parent at
// /home/george/Projects/rapids with a branch worktree), but uses
// tempdirs so the suite stays hermetic.
func TestEnsureWorktreeEnv_isolatedDBOverrideSkipped(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	mainEnv := "APP_URL=http://harborlist.test\nDB_DATABASE=rapids\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}
	lerdYAML := "domains:\n  - harborlist\nenv_overrides:\n  DB_DATABASE: \"{{parent}}_{{branch}}\"\n"
	if err := os.WriteFile(filepath.Join(main, ".lerd.yaml"), []byte(lerdYAML), 0644); err != nil {
		t.Fatal(err)
	}
	// Seed the worktree as if `lerd db:isolate` already ran: explicit
	// DB_DATABASE plus db_isolated:true in its .lerd.yaml.
	wtEnv := "APP_URL=http://harborlist.test\nDB_DATABASE=rapids_feat_x\n"
	if err := os.WriteFile(filepath.Join(wt, ".env"), []byte(wtEnv), 0644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWorktreeDBIsolated(wt, true); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-x.harborlist.test", false)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "DB_DATABASE=rapids_feat_x") {
		t.Errorf("isolated DB_DATABASE was clobbered by env_overrides:\n%s", s)
	}
	if strings.Contains(s, "DB_DATABASE=rapids_feat-x") || strings.Contains(s, "DB_DATABASE=_feat-x") {
		t.Errorf("env_overrides template replaced isolated DB:\n%s", s)
	}
	// APP_URL must still be rewritten — only DB_DATABASE is sticky.
	if !strings.Contains(s, "APP_URL=http://feat-x.harborlist.test") {
		t.Errorf("APP_URL rewrite skipped alongside DB_DATABASE:\n%s", s)
	}
}

// TestEnsureWorktreeEnv_envOverridesWinWhenNotIsolated is the symmetric
// invariant: with db_isolated:false (the default), env_overrides for
// DB_DATABASE still apply. This protects users who deliberately template
// per-branch DBs without going through `lerd db:isolate`.
func TestEnsureWorktreeEnv_envOverridesWinWhenNotIsolated(t *testing.T) {
	main := t.TempDir()
	wt := t.TempDir()

	mainEnv := "APP_URL=http://harborlist.test\nDB_DATABASE=rapids\n"
	if err := os.WriteFile(filepath.Join(main, ".env"), []byte(mainEnv), 0644); err != nil {
		t.Fatal(err)
	}
	lerdYAML := "domains:\n  - harborlist\nenv_overrides:\n  DB_DATABASE: \"{{parent}}_{{branch}}\"\n"
	if err := os.WriteFile(filepath.Join(main, ".lerd.yaml"), []byte(lerdYAML), 0644); err != nil {
		t.Fatal(err)
	}

	EnsureWorktreeEnv(main, wt, "feat-x.harborlist.test", false)

	got, err := os.ReadFile(filepath.Join(wt, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "DB_DATABASE=_feat-x") {
		// The exact value depends on whether parent resolves; with no
		// site registered, {{parent}} resolves to "". The point is that
		// env_overrides DID apply (DB_DATABASE changed away from the
		// inherited "rapids").
		if strings.Contains(string(got), "DB_DATABASE=rapids\n") {
			t.Errorf("env_overrides was skipped even though db_isolated is false:\n%s", got)
		}
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
