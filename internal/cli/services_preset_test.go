package cli

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestInstallPresetByName_Unknown(t *testing.T) {
	_, err := InstallPresetByName("does-not-exist")
	if err == nil {
		t.Fatalf("expected error for unknown preset, got nil")
	}
	if !strings.Contains(err.Error(), "unknown preset") {
		t.Errorf("error = %v, want it to mention 'unknown preset'", err)
	}
}

func TestMissingPresetDependencies_BuiltinDepIsSatisfied(t *testing.T) {
	// phpmyadmin depends on mysql which is a built-in service.
	svc, err := config.LoadPreset("phpmyadmin")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	if missing := MissingPresetDependencies(svc); len(missing) != 0 {
		t.Errorf("expected no missing deps for phpmyadmin, got %v", missing)
	}
}

func TestMissingPresetDependencies_CustomDepReportsMissing(t *testing.T) {
	// mongo-express depends on mongo (a custom-only service). Without it
	// installed in this test's empty XDG_CONFIG_HOME, the dep should be flagged.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	svc, err := config.LoadPreset("mongo-express")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	missing := MissingPresetDependencies(svc)
	if len(missing) != 1 || missing[0] != "mongo" {
		t.Errorf("expected missing=[mongo], got %v", missing)
	}
}
