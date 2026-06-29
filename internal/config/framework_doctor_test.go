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
