package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// ── DetectFrameworkForDir ───────────────────────────────────────────────────

func TestDetectFrameworkForDir_FromLerdYAML(t *testing.T) {
	setConfigDir(t)
	dir := t.TempDir()

	// Create a .lerd.yaml with framework: laravel
	proj := &ProjectConfig{Framework: "laravel"}
	data, _ := yaml.Marshal(proj)
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), data, 0644) //nolint:errcheck

	// Create artisan file so Laravel detection works
	os.WriteFile(filepath.Join(dir, "artisan"), []byte("#!/usr/bin/env php"), 0644) //nolint:errcheck

	name, ok := DetectFrameworkForDir(dir)
	if !ok {
		t.Fatal("expected framework to be detected")
	}
	if name != "laravel" {
		t.Errorf("expected laravel, got %q", name)
	}
}

func TestDetectFrameworkForDir_FromFileDetection(t *testing.T) {
	setConfigDir(t)
	dir := t.TempDir()

	// No .lerd.yaml, but artisan file exists — should detect Laravel
	os.WriteFile(filepath.Join(dir, "artisan"), []byte("#!/usr/bin/env php"), 0644) //nolint:errcheck

	name, ok := DetectFrameworkForDir(dir)
	if !ok {
		t.Fatal("expected framework to be detected from artisan file")
	}
	if name != "laravel" {
		t.Errorf("expected laravel, got %q", name)
	}
}

func TestDetectFrameworkForDir_NoMatch(t *testing.T) {
	setConfigDir(t)
	dir := t.TempDir()

	// Empty dir — nothing to detect
	_, ok := DetectFrameworkForDir(dir)
	if ok {
		t.Error("expected no framework detection in empty dir")
	}
}

func TestDetectFrameworkForDir_LerdYAMLTakesPriority(t *testing.T) {
	setConfigDir(t)
	dir := t.TempDir()

	// Install a statamic store definition
	storeDir := StoreFrameworksDir()
	os.MkdirAll(storeDir, 0755) //nolint:errcheck
	fw := &Framework{
		Name:   "statamic",
		Label:  "Statamic",
		Detect: []FrameworkRule{{Composer: "statamic/cms"}},
	}
	fwData, _ := yaml.Marshal(fw)
	os.WriteFile(filepath.Join(storeDir, "statamic@5.yaml"), fwData, 0644) //nolint:errcheck

	// .lerd.yaml says statamic, but dir also has artisan (which matches Laravel)
	proj := &ProjectConfig{Framework: "statamic"}
	data, _ := yaml.Marshal(proj)
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), data, 0644) //nolint:errcheck
	os.WriteFile(filepath.Join(dir, "artisan"), []byte("#!/usr/bin/env php"), 0644) //nolint:errcheck

	name, ok := DetectFrameworkForDir(dir)
	if !ok {
		t.Fatal("expected framework to be detected")
	}
	if name != "statamic" {
		t.Errorf("expected statamic from .lerd.yaml, got %q", name)
	}
}

func TestDetectFrameworkForDir_EmbeddedDefRestored(t *testing.T) {
	setConfigDir(t)
	dir := t.TempDir()

	// .lerd.yaml with framework + embedded def, but no local definition exists
	proj := &ProjectConfig{
		Framework: "custom-fw",
		FrameworkDef: &Framework{
			Name:      "custom-fw",
			Label:     "Custom FW",
			PublicDir: "public",
			Detect:    []FrameworkRule{{File: "custom.lock"}},
		},
	}
	data, _ := yaml.Marshal(proj)
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), data, 0644) //nolint:errcheck

	name, ok := DetectFrameworkForDir(dir)
	if !ok {
		t.Fatal("expected framework to be detected from embedded def")
	}
	if name != "custom-fw" {
		t.Errorf("expected custom-fw, got %q", name)
	}

	// Verify the embedded def was saved to the store dir
	storePath := filepath.Join(StoreFrameworksDir(), "custom-fw.yaml")
	if _, err := os.Stat(storePath); os.IsNotExist(err) {
		t.Error("embedded framework def should have been saved to store dir")
	}
}

func TestDetectFrameworkForDir_LerdYAML_UnknownFramework(t *testing.T) {
	setConfigDir(t)
	dir := t.TempDir()

	// .lerd.yaml references a framework that doesn't exist anywhere
	proj := &ProjectConfig{Framework: "nonexistent"}
	data, _ := yaml.Marshal(proj)
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), data, 0644) //nolint:errcheck

	_, ok := DetectFrameworkForDir(dir)
	if ok {
		t.Error("should not detect a framework that doesn't exist anywhere")
	}
}
