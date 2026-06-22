package siteinfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// A guessed (clamped) framework definition must not clamp the PHP version during
// snapshot enrichment: a legacy Laravel 6 project pinned to 7.4 via composer
// stays on 7.4 instead of being bumped to the Laravel 10 minimum on every
// snapshot. An exact-match framework still clamps normally.
func TestEnrichVersions_GuessedFrameworkSkipsPHPClamp(t *testing.T) {
	stubPodman(t)
	dir := t.TempDir()
	composer := `{"require":{"php":"^7.4","laravel/framework":"^6.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0644); err != nil {
		t.Fatalf("write composer: %v", err)
	}

	guessed := &config.Framework{
		Name:           "laravel",
		Version:        "10",
		VersionGuessed: true,
		PHP:            config.FrameworkPHP{Min: "8.1", Max: "8.3"},
	}
	e := &EnrichedSite{Name: "legacy", Path: dir, PHPVersion: "7.4"}
	s := config.Site{Name: "legacy", Path: dir, PHPVersion: "7.4", Framework: "laravel"}
	e.enrichVersions(s, guessed, true)

	if e.PHPVersion != "7.4" {
		t.Errorf("PHPVersion = %q, want 7.4 (guessed framework must not clamp)", e.PHPVersion)
	}
	if e.PHPVersionChanged {
		t.Error("PHPVersionChanged = true, want false (7.4 should be left as-is)")
	}
}

// A guessed framework is labelled by the project's detected version, not the
// borrowed definition's version: a Laravel 6 project reads "Laravel 6".
func TestFrameworkLabel_GuessedUsesDetectedVersion(t *testing.T) {
	guessed := &config.Framework{
		Name:            "laravel",
		Label:           "Laravel",
		Version:         "10",
		DetectedVersion: "6",
		VersionGuessed:  true,
	}
	if got := frameworkLabel("laravel", "", guessed, true); got != "Laravel 6" {
		t.Errorf("frameworkLabel(guessed) = %q, want %q", got, "Laravel 6")
	}

	exact := &config.Framework{Name: "laravel", Label: "Laravel", Version: "10"}
	if got := frameworkLabel("laravel", "", exact, true); got != "Laravel 10" {
		t.Errorf("frameworkLabel(exact) = %q, want %q", got, "Laravel 10")
	}
}

// An exact-match framework with a PHP range still clamps an out-of-range pin.
func TestEnrichVersions_ExactFrameworkClamps(t *testing.T) {
	stubPodman(t)
	dir := t.TempDir()
	composer := `{"require":{"php":"^7.4","laravel/framework":"^10.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0644); err != nil {
		t.Fatalf("write composer: %v", err)
	}

	exact := &config.Framework{
		Name:    "laravel",
		Version: "10",
		PHP:     config.FrameworkPHP{Min: "8.1", Max: "8.3"},
	}
	e := &EnrichedSite{Name: "modern", Path: dir, PHPVersion: "7.4"}
	s := config.Site{Name: "modern", Path: dir, PHPVersion: "7.4", Framework: "laravel"}
	e.enrichVersions(s, exact, true)

	// 7.4 is below the 8.1 minimum, so it must be clamped up out of 7.4.
	if e.PHPVersion == "7.4" {
		t.Error("PHPVersion = 7.4, want a clamped version within 8.1-8.3 for an exact framework")
	}
}
