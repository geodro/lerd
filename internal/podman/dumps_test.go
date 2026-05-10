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
// throwaway tempdir. The default DataDir() under $HOME would otherwise mix
// real and test state and tests on a developer machine would be flaky.
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
	if !strings.Contains(string(php), "namespace Lerd\\DumpBridge") {
		t.Errorf("bridge content missing namespace: %s", string(php)[:min(80, len(string(php)))])
	}
	if !strings.Contains(string(php), "function dump") {
		t.Errorf("bridge content missing dump() definition")
	}

	ini, err := os.ReadFile(config.DumpsIniFile())
	if err != nil {
		t.Fatalf("read ini: %v", err)
	}
	if !strings.Contains(string(ini), "auto_prepend_file=") {
		t.Errorf("ini missing auto_prepend_file: %s", string(ini))
	}
	if !strings.Contains(string(ini), "lerd.dump_host=") {
		t.Errorf("ini missing lerd.dump_host: %s", string(ini))
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
	// Simulate podman auto-creating a directory at the bridge path on a
	// previous failed start. WriteDumpBridgeAssets must remove and replace.
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
	if err := RemoveDumpAssets(); err != nil {
		t.Fatalf("RemoveDumpAssets: %v", err)
	}
	if _, err := os.Stat(config.DumpsBridgeFile()); !os.IsNotExist(err) {
		t.Errorf("bridge still present: %v", err)
	}
	if _, err := os.Stat(config.DumpsIniFile()); !os.IsNotExist(err) {
		t.Errorf("ini still present: %v", err)
	}
	// Calling twice is fine.
	if err := RemoveDumpAssets(); err != nil {
		t.Errorf("second remove: %v", err)
	}
}

func TestApplyDumpVolumes_InjectsAfterUserIni(t *testing.T) {
	tmpl := strings.Join([]string{
		"[Container]",
		"Image=lerd-php83-fpm:local",
		"Volume=%h/.local/share/lerd/hosts:/etc/hosts:ro,z",
		"Volume=%h:%h:rw",
		"Volume=/host/99-xdebug.ini:/usr/local/etc/php/conf.d/99-xdebug.ini:ro",
		"Volume=/host/98-user.ini:/usr/local/etc/php/conf.d/98-lerd-user.ini:ro",
		"PodmanArgs=--security-opt=label=disable",
		"Exec=php-fpm -F -R",
	}, "\n")

	got := ApplyDumpVolumes(tmpl, true)
	if !strings.Contains(got, containerDumpBridgePath) {
		t.Errorf("bridge mount missing: %s", got)
	}
	if !strings.Contains(got, containerDumpIniPath) {
		t.Errorf("ini mount missing: %s", got)
	}
	// Bridge lines must appear after the user-ini Volume and before PodmanArgs.
	bridgeIdx := strings.Index(got, containerDumpBridgePath)
	userIdx := strings.Index(got, "98-lerd-user.ini:ro")
	podmanIdx := strings.Index(got, "PodmanArgs=")
	if !(userIdx < bridgeIdx && bridgeIdx < podmanIdx) {
		t.Errorf("bridge volume not inserted between user-ini and PodmanArgs:\n%s", got)
	}
}

func TestApplyDumpVolumes_DisabledStripsExisting(t *testing.T) {
	tmpl := strings.Join([]string{
		"[Container]",
		"Volume=%h:%h:rw",
		"Volume=/host/98-user.ini:/usr/local/etc/php/conf.d/98-lerd-user.ini:ro",
		"PodmanArgs=--security-opt=label=disable",
	}, "\n")

	enabled := ApplyDumpVolumes(tmpl, true)
	if !strings.Contains(enabled, containerDumpBridgePath) {
		t.Fatal("setup: enabled form should have bridge")
	}
	disabled := ApplyDumpVolumes(enabled, false)
	if strings.Contains(disabled, containerDumpBridgePath) {
		t.Errorf("disabled form still contains bridge volume:\n%s", disabled)
	}
	if strings.Contains(disabled, containerDumpIniPath) {
		t.Errorf("disabled form still contains ini volume:\n%s", disabled)
	}
}

func TestApplyDumpVolumes_Idempotent(t *testing.T) {
	tmpl := strings.Join([]string{
		"[Container]",
		"Volume=%h:%h:rw",
		"Volume=/host/98-user.ini:/usr/local/etc/php/conf.d/98-lerd-user.ini:ro",
	}, "\n")
	first := ApplyDumpVolumes(tmpl, true)
	second := ApplyDumpVolumes(first, true)
	if first != second {
		t.Errorf("ApplyDumpVolumes not idempotent:\n--- 1 ---\n%s\n--- 2 ---\n%s", first, second)
	}
}

func TestApplyDumpVolumes_NoUserIniLineNoOps(t *testing.T) {
	tmpl := "[Container]\nImage=foo\n"
	got := ApplyDumpVolumes(tmpl, true)
	if strings.Contains(got, containerDumpBridgePath) {
		t.Errorf("bridge inserted without anchor line:\n%s", got)
	}
}

func TestEnsureDumpAssets_NoOpWhenDisabled(t *testing.T) {
	withTempXDG(t)
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetDumpsEnabled(false)
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDumpAssets(); err != nil {
		t.Fatalf("EnsureDumpAssets: %v", err)
	}
	if _, err := os.Stat(config.DumpsBridgeFile()); !os.IsNotExist(err) {
		t.Errorf("bridge written when dumps disabled")
	}
}

func TestEnsureDumpAssets_WritesWhenEnabled(t *testing.T) {
	withTempXDG(t)
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetDumpsEnabled(true)
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDumpAssets(); err != nil {
		t.Fatalf("EnsureDumpAssets: %v", err)
	}
	if _, err := os.Stat(config.DumpsBridgeFile()); err != nil {
		t.Errorf("bridge missing after EnsureDumpAssets: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
