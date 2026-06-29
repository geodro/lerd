package sitedoctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func writeEnv(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// laravelLikeFW is a minimal framework whose env key generation drives the
// app_key check, used by the env-facing tests.
func laravelLikeFW() *config.Framework {
	return &config.Framework{
		Name:  "laravel",
		Label: "Laravel",
		Env: config.FrameworkEnvConf{
			File:        ".env",
			ExampleFile: ".env.example",
			KeyGeneration: &config.EnvKeyGeneration{
				EnvKey:  "APP_KEY",
				Command: "key:generate",
			},
		},
	}
}

func TestCheckEnvPresent(t *testing.T) {
	dir := t.TempDir()

	c, _ := checkEnvPresent(dir, ".env")
	if c.Status != StatusFail {
		t.Errorf("missing .env: got %q, want fail", c.Status)
	}

	writeEnv(t, dir, ".env", "APP_KEY=x\n")
	if c, _ := checkEnvPresent(dir, ".env"); c.Status != StatusOK {
		t.Errorf("present .env: got %q, want ok", c.Status)
	}
}

func TestCheckAppKey(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	fw := laravelLikeFW()

	// Missing key → fail with the framework's generation command as the fix.
	writeEnv(t, dir, ".env", "APP_NAME=Acme\nAPP_KEY=\n")
	c, ok := checkAppKey(envPath, fw)
	if !ok || c.Status != StatusFail || c.Fix != "key:generate" {
		t.Errorf("empty APP_KEY: got ok=%v status=%q fix=%q, want true/fail/key:generate", ok, c.Status, c.Fix)
	}

	// Set key → ok, no fix.
	writeEnv(t, dir, ".env", "APP_KEY=base64:abcdef==\n")
	if c, ok := checkAppKey(envPath, fw); !ok || c.Status != StatusOK || c.Fix != "" {
		t.Errorf("set APP_KEY: got ok=%v status=%q fix=%q, want true/ok/none", ok, c.Status, c.Fix)
	}

	// Framework with no key generation → check skipped.
	if _, ok := checkAppKey(envPath, &config.Framework{Name: "wordpress"}); ok {
		t.Error("expected app_key skipped when the framework declares no key generation")
	}
}

func TestCheckEnvDrift(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	examplePath := filepath.Join(dir, ".env.example")

	// No .env.example → not applicable.
	writeEnv(t, dir, ".env", "APP_KEY=x\n")
	if _, ok := checkEnvDrift(dir, envPath, examplePath); ok {
		t.Error("expected drift check skipped when no .env.example")
	}

	// Example declares two keys the .env lacks → warn listing both.
	writeEnv(t, dir, ".env.example", "APP_KEY=\nNEW_ONE=\nNEW_TWO=\n")
	writeEnv(t, dir, ".env", "APP_KEY=x\n")
	c, ok := checkEnvDrift(dir, envPath, examplePath)
	if !ok || c.Status != StatusWarn {
		t.Fatalf("missing keys: got ok=%v status=%q, want true/warn", ok, c.Status)
	}
	if !strings.Contains(c.Detail, "NEW_ONE") || !strings.Contains(c.Detail, "NEW_TWO") {
		t.Errorf("detail should name the missing keys, got %q", c.Detail)
	}

	// All example keys present → ok.
	writeEnv(t, dir, ".env", "APP_KEY=x\nNEW_ONE=1\nNEW_TWO=2\n")
	if c, ok := checkEnvDrift(dir, envPath, examplePath); !ok || c.Status != StatusOK {
		t.Errorf("aligned env: got ok=%v status=%q, want true/ok", ok, c.Status)
	}
}

// TestCheckEnvDrift_classifiesRequiredVsOptional: only no-default keys (plus
// VITE_* the frontend needs) drive the warning; keys read with a default or
// never referenced are optional and must not turn the row red.
func TestCheckEnvDrift_classifiesRequiredVsOptional(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	examplePath := filepath.Join(dir, ".env.example")

	writeEnv(t, dir, ".env.example", "APP_KEY=\nDB_HOST=\nLOG_LEVEL=\nVITE_THING=\nVITE_STALE=\nUNUSED_KEY=\n")
	writeEnv(t, dir, ".env", "")

	mustMkdir(t, filepath.Join(dir, "config"))
	writeEnv(t, dir, filepath.Join("config", "app.php"),
		"<?php return [\n"+
			"  'key' => env('APP_KEY'),\n"+
			"  'host' => env('DB_HOST', '127.0.0.1'),\n"+
			"  'log' => env('LOG_LEVEL', 'debug'),\n"+
			"];\n")

	mustMkdir(t, filepath.Join(dir, "resources", "js"))
	writeEnv(t, dir, filepath.Join("resources", "js", "app.js"),
		"const api = import.meta.env.VITE_THING;\nconsole.log(api);\n")

	c, ok := checkEnvDrift(dir, envPath, examplePath)
	if !ok || c.Status != StatusWarn {
		t.Fatalf("got ok=%v status=%q, want true/warn", ok, c.Status)
	}
	if !strings.Contains(c.Detail, "APP_KEY") || !strings.Contains(c.Detail, "VITE_THING") {
		t.Errorf("detail should name required keys APP_KEY and VITE_THING, got %q", c.Detail)
	}
	for _, opt := range []string{"DB_HOST", "LOG_LEVEL", "UNUSED_KEY", "VITE_STALE"} {
		if strings.Contains(c.Detail, opt) {
			t.Errorf("optional key %q should not appear in the required list: %q", opt, c.Detail)
		}
	}
}

func TestCheckEnvDrift_ignoresCompiledPublicBundle(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	examplePath := filepath.Join(dir, ".env.example")

	writeEnv(t, dir, ".env.example", "VITE_STALE=\n")
	writeEnv(t, dir, ".env", "")

	mustMkdir(t, filepath.Join(dir, "public", "build", "assets"))
	writeEnv(t, dir, filepath.Join("public", "build", "assets", "app-abc123.js"),
		"const x=import.meta.env.VITE_STALE;console.log(x);\n")

	if refs := scanViteEnvRefs(dir); refs["VITE_STALE"] {
		t.Errorf("VITE_STALE in public/ should be ignored, got referenced")
	}

	c, ok := checkEnvDrift(dir, envPath, examplePath)
	if !ok {
		t.Fatalf("expected a drift check result")
	}
	if strings.Contains(c.Detail, "VITE_STALE") && c.Status == StatusWarn {
		t.Errorf("a public/-only VITE_ key must not be flagged required: %q", c.Detail)
	}
}

// TestCheckEnvCombo covers the APP_DEBUG-in-production footgun, expressed as a
// declarative env_combo check.
func TestCheckEnvCombo(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	spec := config.DoctorCheck{
		Name:   "app_debug",
		Type:   "env_combo",
		When:   map[string]string{"APP_ENV": "production"},
		WarnIf: map[string]string{"APP_DEBUG": "true"},
		Detail: "debug leaks",
	}

	// production + debug on → warn (and "1" is treated truthily).
	writeEnv(t, dir, ".env", "APP_ENV=production\nAPP_DEBUG=1\n")
	if c := checkEnvCombo(envPath, spec); c.Status != StatusWarn {
		t.Errorf("prod+debug: got %q, want warn", c.Status)
	}
	// local + debug on → ok (When mismatch).
	writeEnv(t, dir, ".env", "APP_ENV=local\nAPP_DEBUG=true\n")
	if c := checkEnvCombo(envPath, spec); c.Status != StatusOK {
		t.Errorf("local+debug: got %q, want ok", c.Status)
	}
	// production + debug off → ok (WarnIf mismatch).
	writeEnv(t, dir, ".env", "APP_ENV=production\nAPP_DEBUG=false\n")
	if c := checkEnvCombo(envPath, spec); c.Status != StatusOK {
		t.Errorf("prod+nodebug: got %q, want ok", c.Status)
	}
}

// TestCheckSymlink covers the public/storage link, expressed declaratively.
func TestCheckSymlink(t *testing.T) {
	spec := config.DoctorCheck{
		Name: "storage_link", Type: "symlink",
		Link: "public/storage", Target: "storage/app/public", RequiresDir: "public",
		Fix: "storage:link",
	}

	// No target dir → not applicable.
	bare := t.TempDir()
	if _, ok := checkSymlink(bare, spec); ok {
		t.Error("expected skip when the target dir is absent")
	}

	// Target + required dir exist, link missing → warn + fix.
	missing := t.TempDir()
	mustMkdir(t, filepath.Join(missing, "storage", "app", "public"))
	mustMkdir(t, filepath.Join(missing, "public"))
	c, ok := checkSymlink(missing, spec)
	if !ok || c.Status != StatusWarn || c.Fix != "storage:link" {
		t.Errorf("missing link: got ok=%v status=%q fix=%q, want true/warn/storage:link", ok, c.Status, c.Fix)
	}

	// Symlink present → ok.
	linked := t.TempDir()
	mustMkdir(t, filepath.Join(linked, "public"))
	mustMkdir(t, filepath.Join(linked, "storage", "app", "public"))
	if err := os.Symlink("../storage/app/public", filepath.Join(linked, "public", "storage")); err != nil {
		t.Fatal(err)
	}
	if c, ok := checkSymlink(linked, spec); !ok || c.Status != StatusOK {
		t.Errorf("present link: got ok=%v status=%q, want true/ok", ok, c.Status)
	}
}

// TestCheckCommand exercises the generic command check (and runCapture) with
// harmless shell commands rather than a real console.
func TestCheckCommand(t *testing.T) {
	dir := t.TempDir()

	// Output contains the trigger → fail.
	fail := config.DoctorCheck{Name: "migrations", Type: "command", Command: "echo Pending", FailIfOutputContains: "Pending", Fix: "migrate", Detail: "pending"}
	if c := checkCommand(context.Background(), dir, fail); c.Status != StatusFail || c.Fix != "migrate" {
		t.Errorf("pending: got status=%q fix=%q, want fail/migrate", c.Status, c.Fix)
	}

	// Clean output → ok.
	ok := config.DoctorCheck{Name: "migrations", Type: "command", Command: "echo done", FailIfOutputContains: "Pending"}
	if c := checkCommand(context.Background(), dir, ok); c.Status != StatusOK {
		t.Errorf("clean: got %q, want ok", c.Status)
	}

	// Non-zero exit with UnknownOnError → unknown.
	unknown := config.DoctorCheck{Name: "migrations", Type: "command", Command: "exit 1", FailIfOutputContains: "Pending", UnknownOnError: true}
	if c := checkCommand(context.Background(), dir, unknown); c.Status != StatusUnknown {
		t.Errorf("errored: got %q, want unknown", c.Status)
	}
}

func TestCheckNodeDeps(t *testing.T) {
	// node_modules missing → warn.
	dir := t.TempDir()
	if c := checkNodeDeps(dir); c.Status != StatusWarn {
		t.Errorf("no node_modules: got %q, want warn", c.Status)
	}

	// Installed but no lockfile → warn.
	noLock := t.TempDir()
	mustMkdir(t, filepath.Join(noLock, "node_modules"))
	if c := checkNodeDeps(noLock); c.Status != StatusWarn {
		t.Errorf("no lockfile: got %q, want warn", c.Status)
	}

	// Installed + lockfile → ok.
	good := t.TempDir()
	mustMkdir(t, filepath.Join(good, "node_modules"))
	writeEnv(t, good, "package-lock.json", "{}\n")
	if c := checkNodeDeps(good); c.Status != StatusOK {
		t.Errorf("installed+lock: got %q, want ok", c.Status)
	}
}

func TestCheckComposerDeps_noVendor(t *testing.T) {
	dir := t.TempDir()
	c := checkComposerDeps(context.Background(), dir)
	if c.Status != StatusWarn {
		t.Errorf("no vendor: got %q, want warn", c.Status)
	}
	if c.Fix != FixComposerInstall {
		t.Errorf("no vendor fix: got %q, want %q", c.Fix, FixComposerInstall)
	}
}

func TestNodeDeps_fixKey(t *testing.T) {
	dir := t.TempDir()
	if c := checkNodeDeps(dir); c.Fix != FixNpmInstall {
		t.Errorf("no node_modules fix: got %q, want %q", c.Fix, FixNpmInstall)
	}
}

func TestDoctorFixCommands_allowlist(t *testing.T) {
	for _, key := range []string{FixComposerInstall, FixComposerUpdate, FixNpmInstall, FixNpmAuditFix} {
		if DoctorFixCommands[key] == "" {
			t.Errorf("fix key %q has no command in the allowlist", key)
		}
	}
}

func TestComposerLockStale(t *testing.T) {
	if !composerLockStale("./composer.json is valid but your composer.lock file is not up to date") {
		t.Error("expected stale=true when validate flags the lock")
	}
	if composerLockStale("./composer.json is valid") {
		t.Error("expected stale=false on a clean validate")
	}
}

func TestParseComposerAudit(t *testing.T) {
	// Object form, two packages with one advisory each.
	if n := parseComposerAudit(`{"advisories":{"pkg/a":[{}],"pkg/b":[{}]}}`); n != 2 {
		t.Errorf("two advisories: got %d, want 2", n)
	}
	// Object form, one package with two advisories → counts advisories, not packages.
	if n := parseComposerAudit(`{"advisories":{"pkg/a":[{},{}]}}`); n != 2 {
		t.Errorf("two advisories one package: got %d, want 2", n)
	}
	// Empty object → clean.
	if n := parseComposerAudit(`Some warning line
{"advisories":{}}`); n != 0 {
		t.Errorf("clean audit (object): got %d, want 0", n)
	}
	// Empty array form (composer emits [] when there are no advisories) → clean.
	if n := parseComposerAudit(`{"advisories":[],"abandoned":[]}`); n != 0 {
		t.Errorf("clean audit (array): got %d, want 0", n)
	}
	if n := parseComposerAudit("not json"); n != -1 {
		t.Errorf("garbage: got %d, want -1", n)
	}
}

func TestParseNpmAudit(t *testing.T) {
	if n := parseNpmAudit(`{"metadata":{"vulnerabilities":{"total":5}}}`); n != 5 {
		t.Errorf("five vulns: got %d, want 5", n)
	}
	if n := parseNpmAudit(`{"metadata":{"vulnerabilities":{"total":0}}}`); n != 0 {
		t.Errorf("clean: got %d, want 0", n)
	}
	if n := parseNpmAudit("oops"); n != -1 {
		t.Errorf("garbage: got %d, want -1", n)
	}
}

func TestHumanize(t *testing.T) {
	cases := map[string]string{"node_audit": "Node Audit", "php_version": "Php Version", "migrations": "Migrations"}
	for in, want := range cases {
		if got := humanize(in); got != want {
			t.Errorf("humanize(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestApplyLabels(t *testing.T) {
	resp := Response{Checks: []Check{
		{Name: "node_audit"},
		{Name: "composer_deps"},
		{Name: "migrations", Label: "Migrations"},
		{Name: "custom_thing"},
	}}
	applyLabels(&resp)
	want := []string{"Node Audit", "Composer Dependencies", "Migrations", "Custom Thing"}
	for i, w := range want {
		if resp.Checks[i].Label != w {
			t.Errorf("check %d label=%q, want %q", i, resp.Checks[i].Label, w)
		}
	}
}

func TestValueMatches(t *testing.T) {
	cases := []struct {
		actual, expected string
		want             bool
	}{
		{"true", "true", true},
		{"1", "true", true},
		{"on", "true", true},
		{"false", "true", false},
		{"", "false", true},
		{"production", "production", true},
		{"Production", "production", true},
		{"local", "production", false},
	}
	for _, c := range cases {
		if got := valueMatches(c.actual, c.expected); got != c.want {
			t.Errorf("valueMatches(%q,%q)=%v, want %v", c.actual, c.expected, got, c.want)
		}
	}
}

func TestCheckPHPVersion(t *testing.T) {
	fw := &config.Framework{Label: "Laravel", PHP: config.FrameworkPHP{Min: "8.3", Max: "8.5"}}

	below := t.TempDir()
	writeEnv(t, below, ".php-version", "8.0\n")
	if c, ok := checkPHPVersion(below, fw); !ok || c.Status != StatusWarn {
		t.Errorf("below min: got ok=%v status=%q, want true/warn", ok, c.Status)
	}

	inRange := t.TempDir()
	writeEnv(t, inRange, ".php-version", "8.4\n")
	if c, ok := checkPHPVersion(inRange, fw); !ok || c.Status != StatusOK {
		t.Errorf("in range: got ok=%v status=%q, want true/ok", ok, c.Status)
	}

	above := t.TempDir()
	writeEnv(t, above, ".php-version", "8.9\n")
	if c, ok := checkPHPVersion(above, fw); !ok || c.Status != StatusWarn {
		t.Errorf("above max: got ok=%v status=%q, want true/warn", ok, c.Status)
	}

	// No declared range → skipped.
	if _, ok := checkPHPVersion(inRange, &config.Framework{}); ok {
		t.Error("expected php_version skipped when the framework declares no range")
	}
}

// TestRun_frameworkAgnostic: a non-Laravel framework with no key generation
// still gets the universal env baseline (env_present + env_drift), proving the
// engine no longer hard-codes Laravel.
func TestRun_frameworkAgnostic(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, ".env", "APP_ENV=dev\n")
	writeEnv(t, dir, ".env.example", "APP_ENV=\nSECRET=\n")

	fw := &config.Framework{
		Name:  "generic",
		Label: "Generic",
		Env:   config.FrameworkEnvConf{File: ".env", ExampleFile: ".env.example"},
	}
	resp := Run(context.Background(), dir, fw)

	names := map[string]string{}
	for _, c := range resp.Checks {
		names[c.Name] = c.Status
	}
	if _, ok := names["env_present"]; !ok {
		t.Error("expected env_present to run for a non-Laravel framework")
	}
	if names["env_drift"] == "" {
		t.Error("expected env_drift to run for a non-Laravel framework")
	}
	if _, ok := names["app_key"]; ok {
		t.Error("app_key must be skipped for a framework with no key generation")
	}
}

// TestRun_phpConstFramework: a WordPress-style framework that stores config in
// wp-config.php (php-const) must not be flagged for a missing .env, and the
// dotenv-only checks must be skipped.
func TestRun_phpConstFramework(t *testing.T) {
	dir := t.TempDir()
	writeEnv(t, dir, "wp-config.php", "<?php define('DB_NAME', 'wp');\n")

	fw := &config.Framework{
		Name:  "wordpress",
		Label: "WordPress",
		Env: config.FrameworkEnvConf{
			FallbackFile:   "wp-config.php",
			FallbackFormat: "php-const",
			Services:       map[string]config.FrameworkServiceDef{"mysql": {}},
		},
	}
	resp := Run(context.Background(), dir, fw)

	names := map[string]string{}
	for _, c := range resp.Checks {
		names[c.Name] = c.Status
	}
	if names["env_present"] != StatusOK {
		t.Errorf("wp-config.php present should pass env_present, got %q", names["env_present"])
	}
	if _, ok := names["env_drift"]; ok {
		t.Error("env_drift must be skipped for a php-const framework")
	}
}
