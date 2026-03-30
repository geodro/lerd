package php

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeFile is a test helper that writes content to filename inside dir.
func writeFile(t *testing.T, dir, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ── parseComposerPHP ──────────────────────────────────────────────────────────

func TestParseComposerPHP(t *testing.T) {
	cases := []struct{ constraint, want string }{
		{"^8.1", "8.1"},
		{"^8.2", "8.2"},
		{">=8.1", "8.1"},
		{">=8.3", "8.3"},
		{"~8.3.0", "8.3"},
		{"^8.4.0", "8.4"},
		{"8.2.*", "8.2"},
		{"*", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := parseComposerPHP(c.constraint)
		if got != c.want {
			t.Errorf("parseComposerPHP(%q) = %q, want %q", c.constraint, got, c.want)
		}
	}
}

// ── DetectVersion ─────────────────────────────────────────────────────────────

func TestDetectVersion_DotLerdYaml(t *testing.T) {
	dir := t.TempDir()
	// All lower-priority files present too — .lerd.yaml must win
	writeFile(t, dir, ".lerd.yaml", "php_version: \"8.1\"\n")
	writeFile(t, dir, ".php-version", "8.2")
	writeFile(t, dir, "composer.json", `{"require":{"php":"^8.3"}}`)

	// Point XDG dirs away from real config
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	got, err := DetectVersion(dir)
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if got != "8.1" {
		t.Errorf("expected 8.1 from .lerd.yaml, got %q", got)
	}
}

func TestDetectVersion_DotPhpVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".php-version", "8.2\n")
	writeFile(t, dir, "composer.json", `{"require":{"php":"^8.3"}}`)

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	got, err := DetectVersion(dir)
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if got != "8.2" {
		t.Errorf("expected 8.2 from .php-version, got %q", got)
	}
}

func TestDetectVersion_ComposerJson(t *testing.T) {
	dir := t.TempDir()
	composer := map[string]interface{}{
		"require": map[string]string{
			"php":           "^8.3",
			"laravel/frame": "^11.0",
		},
	}
	data, _ := json.Marshal(composer)
	os.WriteFile(filepath.Join(dir, "composer.json"), data, 0644)

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	got, err := DetectVersion(dir)
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if got != "8.3" {
		t.Errorf("expected 8.3 from composer.json, got %q", got)
	}
}

func TestDetectVersion_GlobalFallback(t *testing.T) {
	dir := t.TempDir() // no version files

	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// Write a global config with a known PHP version
	cfgContent := "php:\n  default_version: \"8.4\"\n"
	lerdCfgDir := filepath.Join(cfgDir, "lerd")
	os.MkdirAll(lerdCfgDir, 0755)
	os.WriteFile(filepath.Join(lerdCfgDir, "config.yaml"), []byte(cfgContent), 0644)

	got, err := DetectVersion(dir)
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if got != "8.4" {
		t.Errorf("expected 8.4 from global config, got %q", got)
	}
}

func TestDetectVersion_NoFiles_ReturnsDefault(t *testing.T) {
	dir := t.TempDir()

	// Empty config dir — LoadGlobal returns built-in defaults
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	got, err := DetectVersion(dir)
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if got == "" {
		t.Error("expected a non-empty default PHP version")
	}
}

// ── DetectExtensions ──────────────────────────────────────────────────────────

func TestDetectExtensions(t *testing.T) {
	dir := t.TempDir()
	composer := map[string]interface{}{
		"require": map[string]string{
			"php":          "^8.2",
			"ext-redis":    "*",
			"ext-imagick":  "*",
			"ext-pdo":      "*",
			"laravel/some": "^11",
		},
	}
	data, _ := json.Marshal(composer)
	os.WriteFile(filepath.Join(dir, "composer.json"), data, 0644)

	exts := DetectExtensions(dir)
	extSet := make(map[string]bool)
	for _, e := range exts {
		extSet[e] = true
	}

	for _, want := range []string{"redis", "imagick", "pdo"} {
		if !extSet[want] {
			t.Errorf("expected extension %q in %v", want, exts)
		}
	}
	// Non-ext- keys must not appear
	for _, notwant := range []string{"php", "laravel/some"} {
		if extSet[notwant] {
			t.Errorf("unexpected entry %q in extensions %v", notwant, exts)
		}
	}
}

func TestDetectExtensions_NoComposerJson(t *testing.T) {
	dir := t.TempDir()
	exts := DetectExtensions(dir)
	if exts != nil {
		t.Errorf("expected nil when no composer.json, got %v", exts)
	}
}

func TestDetectExtensions_NoExtRequires(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "composer.json", `{"require":{"php":"^8.2","laravel/framework":"^11"}}`)
	exts := DetectExtensions(dir)
	if len(exts) != 0 {
		t.Errorf("expected no extensions, got %v", exts)
	}
}
