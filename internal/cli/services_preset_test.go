package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestInstallPresetByName_Unknown(t *testing.T) {
	_, err := InstallPresetByName("does-not-exist", "")
	if err == nil {
		t.Fatalf("expected error for unknown preset, got nil")
	}
	if !strings.Contains(err.Error(), "unknown preset") {
		t.Errorf("error = %v, want it to mention 'unknown preset'", err)
	}
}

func TestMissingPresetDependencies_BuiltinDepIsSatisfied(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// MissingPresetDependencies now treats built-ins as installed only
	// when their quadlet is on disk (see the reinstall transactionality
	// PR). Materialise a lerd-mysql.container so the dep counts as
	// satisfied — matches the post-`lerd install` state on a real host.
	qdir := config.QuadletDir()
	if err := os.MkdirAll(qdir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(qdir, "lerd-mysql.container"), []byte("[Container]\nImage=docker.io/library/mysql:8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	preset, err := config.LoadPreset("phpmyadmin")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	svc, err := preset.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if missing := MissingPresetDependencies(svc); len(missing) != 0 {
		t.Errorf("expected no missing deps for phpmyadmin, got %v", missing)
	}
}

func TestMissingPresetDependencies_CustomDepReportsMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	preset, err := config.LoadPreset("mongo-express")
	if err != nil {
		t.Fatalf("LoadPreset: %v", err)
	}
	svc, err := preset.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	missing := MissingPresetDependencies(svc)
	if len(missing) != 1 || missing[0] != "mongo" {
		t.Errorf("expected missing=[mongo], got %v", missing)
	}
}
