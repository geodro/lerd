package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// ── Logs field on built-in Laravel ───────────────────────────────────────────

func TestLaravelBuiltinHasLogs(t *testing.T) {
	if len(laravelFramework.Logs) == 0 {
		t.Fatal("built-in Laravel should have Logs configured")
	}
	if laravelFramework.Logs[0].Path != "storage/logs/*.log" {
		t.Errorf("expected storage/logs/*.log, got %s", laravelFramework.Logs[0].Path)
	}
	if laravelFramework.Logs[0].Format != "monolog" {
		t.Errorf("expected monolog format, got %s", laravelFramework.Logs[0].Format)
	}
}

func TestGetFrameworkLaravel_BuiltinLogs(t *testing.T) {
	setConfigDir(t)

	fw, ok := GetFramework("laravel")
	if !ok {
		t.Fatal("expected to find laravel framework")
	}
	if len(fw.Logs) == 0 {
		t.Fatal("GetFramework(laravel) should include built-in Logs")
	}
	if fw.Logs[0].Format != "monolog" {
		t.Errorf("expected monolog, got %s", fw.Logs[0].Format)
	}
}

func TestGetFrameworkLaravel_UserOverridesLogs(t *testing.T) {
	setConfigDir(t)

	// Write a user laravel.yaml that overrides logs
	dir := FrameworksDir()
	os.MkdirAll(dir, 0755)

	userFw := Framework{
		Name: "laravel",
		Logs: []FrameworkLogSource{
			{Path: "storage/logs/*.log", Format: "monolog"},
			{Path: "storage/logs/custom/*.log", Format: "monolog"},
		},
	}
	data, _ := yaml.Marshal(userFw)
	os.WriteFile(filepath.Join(dir, "laravel.yaml"), data, 0644)

	fw, ok := GetFramework("laravel")
	if !ok {
		t.Fatal("expected to find laravel")
	}
	if len(fw.Logs) != 2 {
		t.Fatalf("expected 2 log sources from user override, got %d", len(fw.Logs))
	}
	if fw.Logs[1].Path != "storage/logs/custom/*.log" {
		t.Errorf("second log source path = %q", fw.Logs[1].Path)
	}
}

func TestGetFrameworkLaravel_NoUserOverrideKeepsBuiltinLogs(t *testing.T) {
	setConfigDir(t)

	// Write a user laravel.yaml with only workers, no logs
	dir := FrameworksDir()
	os.MkdirAll(dir, 0755)

	userFw := Framework{
		Name: "laravel",
		Workers: map[string]FrameworkWorker{
			"horizon": {Label: "Horizon", Command: "php artisan horizon"},
		},
	}
	data, _ := yaml.Marshal(userFw)
	os.WriteFile(filepath.Join(dir, "laravel.yaml"), data, 0644)

	fw, ok := GetFramework("laravel")
	if !ok {
		t.Fatal("expected to find laravel")
	}
	// Built-in logs should remain since user didn't override
	if len(fw.Logs) != 1 {
		t.Fatalf("expected 1 built-in log source, got %d", len(fw.Logs))
	}
	if fw.Logs[0].Path != "storage/logs/*.log" {
		t.Errorf("expected built-in log path, got %s", fw.Logs[0].Path)
	}
}

// ── Custom framework with Logs ───────────────────────────────────────────────

func TestGetFrameworkCustom_WithLogs(t *testing.T) {
	setConfigDir(t)

	dir := FrameworksDir()
	os.MkdirAll(dir, 0755)

	fw := Framework{
		Name:      "symfony",
		Label:     "Symfony",
		PublicDir: "public",
		Detect:    []FrameworkRule{{File: "symfony.lock"}},
		Logs: []FrameworkLogSource{
			{Path: "var/log/*.log", Format: "raw"},
		},
	}
	data, _ := yaml.Marshal(fw)
	os.WriteFile(filepath.Join(dir, "symfony.yaml"), data, 0644)

	got, ok := GetFramework("symfony")
	if !ok {
		t.Fatal("expected to find symfony")
	}
	if len(got.Logs) != 1 {
		t.Fatalf("expected 1 log source, got %d", len(got.Logs))
	}
	if got.Logs[0].Path != "var/log/*.log" {
		t.Errorf("log path = %q", got.Logs[0].Path)
	}
	if got.Logs[0].Format != "raw" {
		t.Errorf("log format = %q", got.Logs[0].Format)
	}
}

func TestGetFrameworkCustom_WithoutLogs(t *testing.T) {
	setConfigDir(t)

	dir := FrameworksDir()
	os.MkdirAll(dir, 0755)

	fw := Framework{
		Name:      "wordpress",
		Label:     "WordPress",
		PublicDir: ".",
		Detect:    []FrameworkRule{{File: "wp-login.php"}},
	}
	data, _ := yaml.Marshal(fw)
	os.WriteFile(filepath.Join(dir, "wordpress.yaml"), data, 0644)

	got, ok := GetFramework("wordpress")
	if !ok {
		t.Fatal("expected to find wordpress")
	}
	if len(got.Logs) != 0 {
		t.Errorf("expected 0 log sources for wordpress, got %d", len(got.Logs))
	}
}

// ── SaveFramework preserves Logs ─────────────────────────────────────────────

func TestSaveFrameworkLaravel_PersistsLogs(t *testing.T) {
	setConfigDir(t)

	fw := &Framework{
		Name: "laravel",
		Workers: map[string]FrameworkWorker{
			"horizon": {Label: "Horizon", Command: "php artisan horizon"},
		},
		Logs: []FrameworkLogSource{
			{Path: "storage/logs/*.log", Format: "monolog"},
			{Path: "storage/logs/jobs/*.log", Format: "monolog"},
		},
	}
	if err := SaveFramework(fw); err != nil {
		t.Fatal(err)
	}

	// Read back raw YAML to verify logs are persisted
	path := filepath.Join(FrameworksDir(), "laravel.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Framework
	if err := yaml.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Logs) != 2 {
		t.Fatalf("expected 2 log sources saved, got %d", len(saved.Logs))
	}
}

func TestSaveFrameworkCustom_PersistsLogs(t *testing.T) {
	setConfigDir(t)

	fw := &Framework{
		Name:      "drupal",
		Label:     "Drupal",
		PublicDir: "web",
		Logs: []FrameworkLogSource{
			{Path: "sites/default/files/logs/*.log"},
		},
	}
	if err := SaveFramework(fw); err != nil {
		t.Fatal(err)
	}

	got, ok := GetFramework("drupal")
	if !ok {
		t.Fatal("expected to find drupal after save")
	}
	if len(got.Logs) != 1 {
		t.Fatalf("expected 1 log source, got %d", len(got.Logs))
	}
}

// ── ListFrameworks includes Logs ─────────────────────────────────────────────

func TestListFrameworks_IncludesLogs(t *testing.T) {
	setConfigDir(t)

	frameworks := ListFrameworks()
	// At minimum the built-in Laravel
	found := false
	for _, fw := range frameworks {
		if fw.Name == "laravel" {
			found = true
			if len(fw.Logs) == 0 {
				t.Error("ListFrameworks: laravel should have Logs")
			}
		}
	}
	if !found {
		t.Error("ListFrameworks should include laravel")
	}
}

// ── RemoveFramework ─────────────────────────────────────────────────────────

func TestRemoveFramework_UserDefined(t *testing.T) {
	setConfigDir(t)

	dir := FrameworksDir()
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "myfw.yaml"), []byte("name: myfw\n"), 0644)

	if err := RemoveFramework("myfw"); err != nil {
		t.Fatalf("RemoveFramework(user): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "myfw.yaml")); !os.IsNotExist(err) {
		t.Error("expected user file to be removed")
	}
}

func TestRemoveFramework_StoreInstalled(t *testing.T) {
	setConfigDir(t)

	dir := StoreFrameworksDir()
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "symfony.yaml"), []byte("name: symfony\n"), 0644)
	os.WriteFile(filepath.Join(dir, "symfony@7.yaml"), []byte("name: symfony\nversion: \"7\"\n"), 0644)

	if err := RemoveFramework("symfony"); err != nil {
		t.Fatalf("RemoveFramework(store): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "symfony.yaml")); !os.IsNotExist(err) {
		t.Error("expected unversioned store file to be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "symfony@7.yaml")); !os.IsNotExist(err) {
		t.Error("expected versioned store file to be removed")
	}
}

func TestRemoveFramework_NotFound(t *testing.T) {
	setConfigDir(t)

	err := RemoveFramework("nonexistent")
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got: %v", err)
	}
}

// ── FrameworkLogSource YAML round-trip ────────────────────────────────────────

func TestFrameworkLogSource_YAMLRoundTrip(t *testing.T) {
	original := []FrameworkLogSource{
		{Path: "storage/logs/*.log", Format: "monolog"},
		{Path: "var/log/*.log", Format: "raw"},
		{Path: "logs/*.txt"}, // no format
	}

	data, err := yaml.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	var loaded []FrameworkLogSource
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 3 {
		t.Fatalf("expected 3, got %d", len(loaded))
	}
	if loaded[0].Path != "storage/logs/*.log" || loaded[0].Format != "monolog" {
		t.Errorf("entry 0: %+v", loaded[0])
	}
	if loaded[1].Format != "raw" {
		t.Errorf("entry 1 format: %q", loaded[1].Format)
	}
	if loaded[2].Format != "" {
		t.Errorf("entry 2 format should be empty, got %q", loaded[2].Format)
	}
}
