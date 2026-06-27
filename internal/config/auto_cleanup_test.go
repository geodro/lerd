package config

import (
	"os"
	"testing"
)

func TestAutoCleanupEnabled_NilAndExplicit(t *testing.T) {
	var nilCfg *GlobalConfig
	if !nilCfg.AutoCleanupEnabled() {
		t.Error("nil config should report auto-cleanup on")
	}
	if (&GlobalConfig{AutoCleanup: true}).AutoCleanupEnabled() != true {
		t.Error("AutoCleanup=true should report on")
	}
	if (&GlobalConfig{AutoCleanup: false}).AutoCleanupEnabled() != false {
		t.Error("AutoCleanup=false should report off")
	}
}

// An existing config that predates the auto_cleanup key must default on, which
// relies on defaultConfig seeding true and viper merging only present keys.
func TestLoadGlobal_AutoCleanupDefaultsOnWhenKeyAbsent(t *testing.T) {
	setConfigDir(t)
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GlobalConfigFile(), []byte("php:\n  default_version: \"8.4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateGlobalCache()

	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoCleanupEnabled() {
		t.Error("auto_cleanup absent from config should default on")
	}
}

func TestLoadGlobal_AutoCleanupHonoursDisable(t *testing.T) {
	setConfigDir(t)
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GlobalConfigFile(), []byte("auto_cleanup: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	invalidateGlobalCache()

	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutoCleanupEnabled() {
		t.Error("auto_cleanup: false should disable the sweep")
	}
}
