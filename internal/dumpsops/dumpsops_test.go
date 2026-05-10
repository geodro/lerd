package dumpsops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// withTempXDG isolates the test from the developer's real lerd state by
// remapping HOME and the XDG dirs into a fresh temp dir.
func withTempXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	if err := os.MkdirAll(config.QuadletDir(), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// stubReload replaces podman.DaemonReloadFn for the test. We don't want
// dumpsops shelling out to systemctl in unit tests; the real reload is
// already covered by the podman integration tests.
func stubReload(t *testing.T) {
	t.Helper()
	prev := podman.DaemonReloadFn
	podman.DaemonReloadFn = func() error { return nil }
	t.Cleanup(func() { podman.DaemonReloadFn = prev })
}

// fakeLifecycle is the minimum stub needed to redirect podman.RestartUnit
// away from the real systemd DBus during tests. Apply must not require an
// actual systemd to succeed; restart errors are surfaced via Result.RestartErr.
type fakeLifecycle struct {
	restartErr error
	restarted  []string
}

func (f *fakeLifecycle) Start(_ string) error                { return nil }
func (f *fakeLifecycle) Stop(_ string) error                 { return nil }
func (f *fakeLifecycle) UnitStatus(_ string) (string, error) { return "active", nil }
func (f *fakeLifecycle) AllUnitStates() map[string]string    { return map[string]string{} }
func (f *fakeLifecycle) Restart(name string) error {
	f.restarted = append(f.restarted, name)
	return f.restartErr
}

func stubLifecycle(t *testing.T, err error) *fakeLifecycle {
	t.Helper()
	fake := &fakeLifecycle{restartErr: err}
	prev := podman.UnitLifecycle
	podman.UnitLifecycle = fake
	t.Cleanup(func() { podman.UnitLifecycle = prev })
	return fake
}

func writePhpQuadlet(t *testing.T, version string) string {
	t.Helper()
	short := strings.ReplaceAll(version, ".", "")
	path := filepath.Join(config.QuadletDir(), "lerd-php"+short+"-fpm.container")
	tmpl, err := podman.GetQuadletTemplate("lerd-php-fpm.container.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	content := strings.ReplaceAll(tmpl, "{{.Version}}", version)
	content = strings.ReplaceAll(content, "{{.VersionShort}}", short)
	content = strings.ReplaceAll(content, "{{.XdebugIniPath}}", config.PHPConfFile(version))
	content = strings.ReplaceAll(content, "{{.UserIniPath}}", config.PHPUserIniFile(version))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestApply_NoChangeWhenAlreadyOff(t *testing.T) {
	withTempXDG(t)
	stubReload(t)
	stubLifecycle(t, nil)

	res, err := Apply(false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.NoChange || res.Enabled {
		t.Errorf("Result = %+v, want NoChange && !Enabled", res)
	}
}

func TestApply_OnInjectsVolumesAndPersistsConfig(t *testing.T) {
	withTempXDG(t)
	stubReload(t)
	stubLifecycle(t, nil)

	path := writePhpQuadlet(t, "8.3")

	res, err := Apply(true)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Enabled {
		t.Errorf("Result.Enabled = false, want true")
	}
	if len(res.Changed) != 1 || !strings.HasSuffix(res.Changed[0], "fpm") {
		t.Errorf("Changed = %v, want [lerd-php83-fpm]", res.Changed)
	}
	if len(res.Restarted) != 1 {
		t.Errorf("Restarted = %v, want one entry", res.Restarted)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "/usr/local/etc/lerd/dump-bridge.php:ro") {
		t.Errorf("FPM quadlet missing bridge volume after enable:\n%s", got)
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsDumpsEnabled() {
		t.Errorf("config not persisted as enabled")
	}

	if _, err := os.Stat(config.DumpsBridgeFile()); err != nil {
		t.Errorf("bridge file missing: %v", err)
	}
}

func TestApply_OffStripsVolumesAndRemovesAssets(t *testing.T) {
	withTempXDG(t)
	stubReload(t)
	stubLifecycle(t, nil)
	path := writePhpQuadlet(t, "8.3")

	if _, err := Apply(true); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(false)
	if err != nil {
		t.Fatalf("Apply off: %v", err)
	}
	if res.Enabled {
		t.Errorf("Result.Enabled = true, want false")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "dump-bridge.php") {
		t.Errorf("bridge volume not stripped on disable:\n%s", got)
	}
	if _, err := os.Stat(config.DumpsBridgeFile()); !os.IsNotExist(err) {
		t.Errorf("bridge file not removed: %v", err)
	}
}

func TestApply_RestartErrSurfaces(t *testing.T) {
	withTempXDG(t)
	stubReload(t)
	stubLifecycle(t, os.ErrPermission)
	writePhpQuadlet(t, "8.3")

	res, err := Apply(true)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.RestartErr == nil {
		t.Errorf("expected RestartErr to be set")
	}
	if len(res.Restarted) != 0 {
		t.Errorf("Restarted = %v, want empty when restart failed", res.Restarted)
	}
}

func TestApply_NoVersionsInstalledStillUpdatesConfig(t *testing.T) {
	withTempXDG(t)
	stubReload(t)
	stubLifecycle(t, nil)

	res, err := Apply(true)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !res.Enabled {
		t.Errorf("Result.Enabled = false, want true")
	}
	if len(res.Changed) != 0 {
		t.Errorf("Changed = %v, want empty (no installed versions)", res.Changed)
	}
}
