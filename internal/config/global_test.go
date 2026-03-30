package config

import (
	"testing"
)

// setConfigDir points ConfigDir() and DataDir() at a temp directory.
func setConfigDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
}

// ── LoadGlobal ────────────────────────────────────────────────────────────────

func TestLoadGlobal_Defaults(t *testing.T) {
	setConfigDir(t)
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.PHP.DefaultVersion == "" {
		t.Error("expected a default PHP version")
	}
	if cfg.DNS.TLD == "" {
		t.Error("expected a default DNS TLD")
	}
	if cfg.Nginx.HTTPPort == 0 {
		t.Error("expected a non-zero HTTP port")
	}
	if cfg.Nginx.HTTPSPort == 0 {
		t.Error("expected a non-zero HTTPS port")
	}
}

func TestSaveLoadGlobal_RoundTrip(t *testing.T) {
	setConfigDir(t)

	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}

	cfg.PHP.DefaultVersion = "8.2"
	cfg.Node.DefaultVersion = "20"
	cfg.DNS.TLD = "local"
	cfg.Nginx.HTTPPort = 8080

	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	got, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal after save: %v", err)
	}
	if got.PHP.DefaultVersion != "8.2" {
		t.Errorf("PHP.DefaultVersion = %q, want %q", got.PHP.DefaultVersion, "8.2")
	}
	if got.Node.DefaultVersion != "20" {
		t.Errorf("Node.DefaultVersion = %q, want %q", got.Node.DefaultVersion, "20")
	}
	if got.DNS.TLD != "local" {
		t.Errorf("DNS.TLD = %q, want %q", got.DNS.TLD, "local")
	}
	if got.Nginx.HTTPPort != 8080 {
		t.Errorf("Nginx.HTTPPort = %d, want 8080", got.Nginx.HTTPPort)
	}
}

// ── Xdebug ────────────────────────────────────────────────────────────────────

func TestXdebug_Toggle(t *testing.T) {
	cfg := &GlobalConfig{}

	if cfg.IsXdebugEnabled("8.3") {
		t.Error("expected xdebug disabled by default")
	}

	cfg.SetXdebug("8.3", true)
	if !cfg.IsXdebugEnabled("8.3") {
		t.Error("expected xdebug enabled after SetXdebug(true)")
	}

	cfg.SetXdebug("8.3", false)
	if cfg.IsXdebugEnabled("8.3") {
		t.Error("expected xdebug disabled after SetXdebug(false)")
	}
}

func TestXdebug_IndependentVersions(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.SetXdebug("8.3", true)
	cfg.SetXdebug("8.4", false)

	if !cfg.IsXdebugEnabled("8.3") {
		t.Error("8.3 should still be enabled")
	}
	if cfg.IsXdebugEnabled("8.4") {
		t.Error("8.4 should remain disabled")
	}
}

// ── Extensions ────────────────────────────────────────────────────────────────

func TestExtensions_AddRemoveGet(t *testing.T) {
	cfg := &GlobalConfig{}

	if exts := cfg.GetExtensions("8.3"); exts != nil {
		t.Errorf("expected nil extensions, got %v", exts)
	}

	cfg.AddExtension("8.3", "redis")
	cfg.AddExtension("8.3", "imagick")

	exts := cfg.GetExtensions("8.3")
	if len(exts) != 2 {
		t.Fatalf("expected 2 extensions, got %d: %v", len(exts), exts)
	}

	cfg.RemoveExtension("8.3", "redis")
	exts = cfg.GetExtensions("8.3")
	if len(exts) != 1 || exts[0] != "imagick" {
		t.Errorf("expected [imagick] after remove, got %v", exts)
	}
}

func TestExtensions_AddIdempotent(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.AddExtension("8.3", "redis")
	cfg.AddExtension("8.3", "redis")

	if len(cfg.GetExtensions("8.3")) != 1 {
		t.Error("duplicate add should be a no-op")
	}
}

func TestExtensions_RemoveLastCleansMap(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.AddExtension("8.3", "redis")
	cfg.RemoveExtension("8.3", "redis")

	if exts := cfg.GetExtensions("8.3"); len(exts) != 0 {
		t.Errorf("expected empty after removing last ext, got %v", exts)
	}
}

func TestExtensions_RemoveNonExistent(t *testing.T) {
	cfg := &GlobalConfig{}
	// Should not panic
	cfg.RemoveExtension("8.3", "nonexistent")
}

func TestExtensions_IndependentVersions(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.AddExtension("8.3", "redis")
	cfg.AddExtension("8.4", "imagick")

	if exts := cfg.GetExtensions("8.3"); len(exts) != 1 || exts[0] != "redis" {
		t.Errorf("8.3 extensions wrong: %v", exts)
	}
	if exts := cfg.GetExtensions("8.4"); len(exts) != 1 || exts[0] != "imagick" {
		t.Errorf("8.4 extensions wrong: %v", exts)
	}
}
