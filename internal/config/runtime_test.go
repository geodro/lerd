package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetProjectRuntimeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	yaml := "domains:\n  - myapp\n"
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SetProjectRuntime(dir, "frankenphp", true); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime != "frankenphp" {
		t.Fatalf("Runtime: want frankenphp, got %s", cfg.Runtime)
	}
	if !cfg.RuntimeWorker {
		t.Fatalf("RuntimeWorker: want true, got false")
	}
	// Clear runtime → worker flag should also go false.
	if err := SetProjectRuntime(dir, "", true); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runtime != "" || cfg.RuntimeWorker {
		t.Fatalf("cleared runtime: got %+v", cfg)
	}
}

func TestSiteIsFrankenPHP(t *testing.T) {
	s := Site{Runtime: "frankenphp"}
	if !s.IsFrankenPHP() {
		t.Fatal("IsFrankenPHP: want true for runtime=frankenphp")
	}
	if s.IsCustomContainer() {
		t.Fatal("IsCustomContainer: want false for FrankenPHP site")
	}
	plain := Site{}
	if plain.IsFrankenPHP() {
		t.Fatal("IsFrankenPHP: want false for empty site")
	}
}

func TestSiteIsCustomFPM(t *testing.T) {
	s := Site{Runtime: "fpm-custom"}
	if !s.IsCustomFPM() {
		t.Fatal("IsCustomFPM: want true for runtime=fpm-custom")
	}
	// A custom-FPM site is a fastcgi PHP site, not a port-based custom container
	// nor a FrankenPHP site.
	if s.IsCustomContainer() || s.IsFrankenPHP() {
		t.Fatal("IsCustomFPM site must not also report IsCustomContainer/IsFrankenPHP")
	}
	plain := Site{}
	if plain.IsCustomFPM() {
		t.Fatal("IsCustomFPM: want false for empty site")
	}
}
