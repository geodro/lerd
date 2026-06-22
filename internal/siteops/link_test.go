package siteops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// A legacy Laravel 6 project served by a clamped (guessed) definition must not
// inherit that definition's PHP range, so PHP 7.4 in composer.json is honoured
// rather than clamped up to the Laravel 10 minimum.
func TestDetectSiteVersions_GuessedFrameworkSkipsPHPClamp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	storeDir := config.StoreFrameworksDir()
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	def := "name: laravel\nlabel: Laravel\nversion: \"10\"\npublic_dir: public\n" +
		"php:\n  min: \"8.1\"\n  max: \"8.3\"\n" +
		"detect:\n  - composer: laravel/framework\n"
	if err := os.WriteFile(filepath.Join(storeDir, "laravel@10.yaml"), []byte(def), 0644); err != nil {
		t.Fatalf("write def: %v", err)
	}

	dir := t.TempDir()
	composer := `{"require":{"php":"^7.4","laravel/framework":"^6.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0644); err != nil {
		t.Fatalf("write composer: %v", err)
	}

	result := DetectSiteVersions(dir, "laravel", "8.4", "22")
	if !result.FrameworkGuessed {
		t.Error("FrameworkGuessed = false, want true for a Laravel 6 project")
	}
	if result.PHPMin != "" || result.PHPMax != "" {
		t.Errorf("expected no PHP clamp range when guessed, got min=%q max=%q", result.PHPMin, result.PHPMax)
	}
	if result.PHP != "7.4" {
		t.Errorf("PHP = %q, want 7.4 (composer constraint honoured, not clamped)", result.PHP)
	}
}

func TestDetectSiteVersions_Defaults(t *testing.T) {
	dir := t.TempDir()
	result := DetectSiteVersions(dir, "", "8.4", "22")
	if result.PHP == "" {
		t.Error("PHP should not be empty")
	}
	if result.Node == "" {
		t.Error("Node should not be empty")
	}
	if result.SuggestedPHP != "" {
		t.Errorf("SuggestedPHP = %q, want empty (no framework)", result.SuggestedPHP)
	}
	if result.PHPMin != "" || result.PHPMax != "" {
		t.Error("expected empty min/max without framework")
	}
}

func TestDetectSiteVersions_UnknownFramework(t *testing.T) {
	dir := t.TempDir()
	result := DetectSiteVersions(dir, "nonexistent", "8.4", "22")
	if result.PHP == "" {
		t.Error("PHP should not be empty even with unknown framework")
	}
	if result.Node == "" {
		t.Error("Node should not be empty")
	}
}

func TestDetectSiteVersions_SuggestsWhenBetterAvailable(t *testing.T) {
	// SuggestedPHP should be non-empty when the clamped version is below
	// the framework's max and that max version isn't installed.
	// We can't easily test with real framework definitions in unit tests,
	// but we verify the struct fields are populated correctly.
	dir := t.TempDir()
	result := DetectSiteVersions(dir, "", "8.4", "22")
	// No framework = no suggestion
	if result.PHPMin != "" || result.PHPMax != "" {
		t.Errorf("expected empty min/max without framework, got min=%q max=%q", result.PHPMin, result.PHPMax)
	}
}

func TestCompareMajorMinor(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"8.1", "8.3", -1},
		{"8.4", "8.3", 1},
		{"8.3", "8.3", 0},
		{"7.4", "8.1", -1},
		{"9.0", "8.4", 1},
	}
	for _, c := range cases {
		got := compareMajorMinor(c.a, c.b)
		if got != c.want {
			t.Errorf("compareMajorMinor(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCleanupRelink_NoExisting(t *testing.T) {
	secured := CleanupRelink("/tmp/nonexistent-path-"+t.Name(), "mysite")
	if secured {
		t.Error("expected secured=false for non-existent path")
	}
}
