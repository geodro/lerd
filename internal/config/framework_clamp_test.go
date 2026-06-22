package config

import (
	"os"
	"path/filepath"
	"testing"
)

// installLaravelVersions writes minimal laravel@<v>.yaml store definitions with
// the given PHP ranges, so GetFrameworkForDir has a versioned set to clamp to.
func installLaravelVersions(t *testing.T, versions map[string][2]string) {
	t.Helper()
	storeDir := StoreFrameworksDir()
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	for v, php := range versions {
		body := "name: laravel\nlabel: Laravel\nversion: \"" + v + "\"\n" +
			"public_dir: public\n" +
			"php:\n  min: \"" + php[0] + "\"\n  max: \"" + php[1] + "\"\n" +
			"detect:\n  - composer: laravel/framework\n"
		path := filepath.Join(storeDir, "laravel@"+v+".yaml")
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func writeComposer(t *testing.T, dir, laravelConstraint string) {
	t.Helper()
	body := `{"require":{"laravel/framework":"` + laravelConstraint + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(body), 0644); err != nil {
		t.Fatalf("write composer.json: %v", err)
	}
}

func TestClampFrameworkVersion(t *testing.T) {
	setConfigDir(t)
	installLaravelVersions(t, map[string][2]string{
		"10": {"8.1", "8.3"},
		"11": {"8.2", "8.4"},
		"12": {"8.2", "8.4"},
		"13": {"8.3", "8.5"},
	})

	cases := []struct {
		detected string
		want     string
	}{
		{"6", "10"}, // below lowest → lowest
		{"9", "10"}, // below lowest → lowest
		{"14", ""},  // above highest → no clamp (falls back to latest)
		{"12", ""},  // in a gap → no clamp (falls back to latest)
		{"11", ""},  // exact exists → no clamp
		{"13", ""},  // exact exists → no clamp
		{"x", ""},   // non-numeric → no clamp
	}
	for _, c := range cases {
		if got := clampFrameworkVersion("laravel", c.detected); got != c.want {
			t.Errorf("clampFrameworkVersion(%q) = %q, want %q", c.detected, got, c.want)
		}
	}
}

func TestClampFrameworkVersion_NoVersionedDefs(t *testing.T) {
	setConfigDir(t)
	if got := clampFrameworkVersion("laravel", "6"); got != "" {
		t.Errorf("clampFrameworkVersion with no defs = %q, want empty", got)
	}
}

// A Laravel 6 project (no laravel@6.yaml) is served by the lowest available
// definition and flagged as guessed.
func TestGetFrameworkForDir_LegacyClampsToLowest(t *testing.T) {
	setConfigDir(t)
	installLaravelVersions(t, map[string][2]string{
		"10": {"8.1", "8.3"},
		"13": {"8.3", "8.5"},
	})
	dir := t.TempDir()
	writeComposer(t, dir, "^6.0")

	fw, ok := GetFrameworkForDir("laravel", dir)
	if !ok {
		t.Fatal("expected a framework definition")
	}
	if fw.Version != "10" {
		t.Errorf("Version = %q, want 10 (lowest available)", fw.Version)
	}
	if !fw.VersionGuessed {
		t.Error("VersionGuessed = false, want true for a clamped legacy project")
	}
	if fw.DetectedVersion != "6" {
		t.Errorf("DetectedVersion = %q, want 6", fw.DetectedVersion)
	}
}

// A future Laravel 14 project falls back to the latest available definition
// (existing behavior) and is NOT flagged as guessed, so PHP stays clamped: its
// composer asks for a high PHP that's inside the latest def's range anyway.
func TestGetFrameworkForDir_FutureFallsBackToLatest(t *testing.T) {
	setConfigDir(t)
	installLaravelVersions(t, map[string][2]string{
		"10": {"8.1", "8.3"},
		"13": {"8.3", "8.5"},
	})
	dir := t.TempDir()
	writeComposer(t, dir, "^14.0")

	fw, ok := GetFrameworkForDir("laravel", dir)
	if !ok {
		t.Fatal("expected a framework definition")
	}
	if fw.Version != "13" {
		t.Errorf("Version = %q, want 13 (latest available)", fw.Version)
	}
	if fw.VersionGuessed {
		t.Error("VersionGuessed = true, want false for a future-version fallback")
	}
}

// An exact match is never flagged as guessed.
func TestGetFrameworkForDir_ExactNotGuessed(t *testing.T) {
	setConfigDir(t)
	installLaravelVersions(t, map[string][2]string{
		"10": {"8.1", "8.3"},
		"11": {"8.2", "8.4"},
		"13": {"8.3", "8.5"},
	})
	dir := t.TempDir()
	writeComposer(t, dir, "^11.0")

	fw, ok := GetFrameworkForDir("laravel", dir)
	if !ok {
		t.Fatal("expected a framework definition")
	}
	if fw.Version != "11" {
		t.Errorf("Version = %q, want 11", fw.Version)
	}
	if fw.VersionGuessed {
		t.Error("VersionGuessed = true, want false for an exact match")
	}
}
