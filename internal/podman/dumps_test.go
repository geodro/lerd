package podman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// withTempXDG redirects XDG_DATA_HOME / XDG_CONFIG_HOME / HOME for the
// duration of the test so config.DataDir / DumpsAssetsDir resolve under a
// throwaway tempdir.
func withTempXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	return dir
}

func TestWriteDumpBridgeAssets_WritesPHPAndIni(t *testing.T) {
	withTempXDG(t)

	if err := WriteDumpBridgeAssets(); err != nil {
		t.Fatalf("WriteDumpBridgeAssets: %v", err)
	}

	php, err := os.ReadFile(config.DumpsBridgeFile())
	if err != nil {
		t.Fatalf("read bridge: %v", err)
	}
	if !strings.Contains(string(php), "enabled.flag") {
		t.Errorf("bridge content missing sentinel check: %s", string(php)[:min(80, len(string(php)))])
	}

	ini, err := os.ReadFile(config.DumpsIniFile())
	if err != nil {
		t.Fatalf("read ini: %v", err)
	}
	if !strings.Contains(string(ini), "auto_prepend_file=") {
		t.Errorf("ini missing auto_prepend_file: %s", string(ini))
	}
}

func TestWriteDumpBridgeAssets_Idempotent(t *testing.T) {
	withTempXDG(t)
	if err := WriteDumpBridgeAssets(); err != nil {
		t.Fatal(err)
	}
	stat1, err := os.Stat(config.DumpsBridgeFile())
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDumpBridgeAssets(); err != nil {
		t.Fatal(err)
	}
	stat2, err := os.Stat(config.DumpsBridgeFile())
	if err != nil {
		t.Fatal(err)
	}
	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Errorf("file rewritten on idempotent call: %v vs %v", stat1.ModTime(), stat2.ModTime())
	}
}

func TestWriteDumpBridgeAssets_ReplacesDirectory(t *testing.T) {
	withTempXDG(t)
	if err := os.MkdirAll(config.DumpsBridgeFile(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteDumpBridgeAssets(); err != nil {
		t.Fatalf("WriteDumpBridgeAssets: %v", err)
	}
	info, err := os.Stat(config.DumpsBridgeFile())
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Errorf("bridge path is still a directory")
	}
}

func TestRemoveDumpAssets(t *testing.T) {
	withTempXDG(t)
	if err := WriteDumpBridgeAssets(); err != nil {
		t.Fatal(err)
	}
	if err := SetDumpsBridgeFlag(true); err != nil {
		t.Fatal(err)
	}
	if err := RemoveDumpAssets(); err != nil {
		t.Fatalf("RemoveDumpAssets: %v", err)
	}
	for _, p := range []string{config.DumpsBridgeFile(), config.DumpsIniFile(), config.DumpsEnabledFlagFile()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still present: %v", filepath.Base(p), err)
		}
	}
	if err := RemoveDumpAssets(); err != nil {
		t.Errorf("second remove: %v", err)
	}
}

func TestSetDumpsBridgeFlag_TogglesFile(t *testing.T) {
	withTempXDG(t)
	if err := SetDumpsBridgeFlag(true); err != nil {
		t.Fatalf("flag on: %v", err)
	}
	if _, err := os.Stat(config.DumpsEnabledFlagFile()); err != nil {
		t.Errorf("flag missing after set true: %v", err)
	}
	if err := SetDumpsBridgeFlag(false); err != nil {
		t.Fatalf("flag off: %v", err)
	}
	if _, err := os.Stat(config.DumpsEnabledFlagFile()); !os.IsNotExist(err) {
		t.Errorf("flag still present after set false")
	}
	// Removing again is a no-op.
	if err := SetDumpsBridgeFlag(false); err != nil {
		t.Errorf("second off: %v", err)
	}
}

func TestEnsureDumpAssets_AlwaysWritesRegardlessOfConfig(t *testing.T) {
	withTempXDG(t)
	cfg, _ := config.LoadGlobal()
	cfg.SetDumpsEnabled(false)
	_ = config.SaveGlobal(cfg)

	if err := EnsureDumpAssets(); err != nil {
		t.Fatalf("EnsureDumpAssets: %v", err)
	}
	if _, err := os.Stat(config.DumpsBridgeFile()); err != nil {
		t.Errorf("bridge missing even though FPM mount needs it: %v", err)
	}
}

func TestFPMQuadletAlwaysMountsBridge(t *testing.T) {
	tmpl, err := GetQuadletTemplate("lerd-php-fpm.container.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tmpl, "{{.DumpsDir}}") {
		t.Errorf("FPM template missing DumpsDir mount placeholder")
	}
	if !strings.Contains(tmpl, "{{.DumpsIniPath}}") {
		t.Errorf("FPM template missing DumpsIniPath mount placeholder")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
