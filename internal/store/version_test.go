package store

import (
	"os"
	"path/filepath"
	"testing"
)

// ── extractMajorVersion ──────────────────────────────────────────────────────

func TestExtractMajorVersion(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"v11.34.2", "11"},
		{"11.34.2", "11"},
		{"7.0.0", "7"},
		{"v2.3.0-beta.1", "2"},
		{"6.0.0-rc.1", "6"},
		{"v1.0.0", "1"},
		{"", ""},
		{"v", ""},
	}

	for _, c := range cases {
		got := extractMajorVersion(c.input)
		if got != c.want {
			t.Errorf("extractMajorVersion(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ── DetectFrameworkVersion ───────────────────────────────────────────────────

func TestDetectFrameworkVersion(t *testing.T) {
	dir := t.TempDir()
	composerLock := `{
		"packages": [
			{"name": "laravel/framework", "version": "v11.34.2"},
			{"name": "guzzlehttp/guzzle", "version": "7.8.1"}
		],
		"packages-dev": [
			{"name": "phpunit/phpunit", "version": "10.5.0"}
		]
	}`
	os.WriteFile(filepath.Join(dir, "composer.lock"), []byte(composerLock), 0o644) //nolint:errcheck

	got := DetectFrameworkVersion(dir, "laravel/framework")
	if got != "11" {
		t.Errorf("DetectFrameworkVersion() = %q, want %q", got, "11")
	}
}

func TestDetectFrameworkVersion_DevPackage(t *testing.T) {
	dir := t.TempDir()
	composerLock := `{
		"packages": [],
		"packages-dev": [
			{"name": "symfony/framework-bundle", "version": "v7.1.0"}
		]
	}`
	os.WriteFile(filepath.Join(dir, "composer.lock"), []byte(composerLock), 0o644) //nolint:errcheck

	got := DetectFrameworkVersion(dir, "symfony/framework-bundle")
	if got != "7" {
		t.Errorf("DetectFrameworkVersion() = %q, want %q", got, "7")
	}
}

func TestDetectFrameworkVersion_NotFound(t *testing.T) {
	dir := t.TempDir()
	composerLock := `{"packages": [{"name": "foo/bar", "version": "1.0.0"}], "packages-dev": []}`
	os.WriteFile(filepath.Join(dir, "composer.lock"), []byte(composerLock), 0o644) //nolint:errcheck

	got := DetectFrameworkVersion(dir, "laravel/framework")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestDetectFrameworkVersion_NoFile(t *testing.T) {
	dir := t.TempDir()
	got := DetectFrameworkVersion(dir, "laravel/framework")
	if got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}
