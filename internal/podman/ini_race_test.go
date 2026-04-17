package podman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// setupConfigHome points config.PHPConfFile / PHPUserIniFile / GlobalConfigFile
// at a temp directory by overriding XDG_CONFIG_HOME and XDG_DATA_HOME. Each test
// gets an isolated tree so concurrent test runs don't collide on the per-version
// ini paths.
//
// PATH is also emptied and the macOS homebrew podman fallbacks are redirected
// so PodmanBin() resolves to a non-existent binary. Any exec.Command podman
// the helpers under test trigger (WriteContainerHosts calls DetectHostGatewayIP
// and nginxContainerIP, which both shell out to podman) fails fast with
// "executable not found" instead of hitting a real podman and polluting the
// temp dir with container-storage overlays that the Go TempDir cleanup then
// fails to remove.
func setupConfigHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("PATH", filepath.Join(tmp, "no-bin"))
}

// writeConfigYAML writes a global config.yaml with the given xdebug-enabled
// state for a single PHP version, so EnsureXdebugIni picks up the right mode.
func writeConfigYAML(t *testing.T, version string, xdebugOn bool) {
	t.Helper()
	cfgPath := config.GlobalConfigFile()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	body := "php:\n  default_version: " + version + "\n"
	if xdebugOn {
		body += "  xdebug_enabled:\n    \"" + version + "\": true\n"
	}
	if err := os.WriteFile(cfgPath, []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// ── EnsureXdebugIni ──────────────────────────────────────────────────────────

func TestEnsureXdebugIni_createsWhenMissing(t *testing.T) {
	// First-install case: nothing on disk yet. EnsureXdebugIni must
	// produce a regular file before WriteFPMQuadlet hands the path to
	// podman as a bind-mount source — otherwise podman auto-creates a
	// directory there and breaks every later WriteXdebugIni.
	setupConfigHome(t)
	writeConfigYAML(t, "8.4", false)

	if err := EnsureXdebugIni("8.4"); err != nil {
		t.Fatalf("EnsureXdebugIni: %v", err)
	}
	info, err := os.Stat(config.PHPConfFile("8.4"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.IsDir() {
		t.Errorf("expected regular file, got directory")
	}
}

func TestEnsureXdebugIni_noopWhenRegularFileExists(t *testing.T) {
	// Regression guard: must not clobber a user-modified ini just because
	// the FPM container was restarted. Anything we'd write would be
	// "off" by default (since cfg.IsXdebugEnabled isn't toggled),
	// silently disabling xdebug when the user had it on via direct edit.
	setupConfigHome(t)
	writeConfigYAML(t, "8.4", false)

	path := config.PHPConfFile("8.4")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	custom := []byte("custom user content\n")
	if err := os.WriteFile(path, custom, 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureXdebugIni("8.4"); err != nil {
		t.Fatalf("EnsureXdebugIni: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Errorf("file was rewritten, want preserved\ngot: %s\nwant: %s", got, custom)
	}
}

func TestEnsureXdebugIni_healsStaleDirectory(t *testing.T) {
	// The bug commit 636bb6e fixes: podman bind-mounted a missing source
	// path and auto-created it as a directory. EnsureXdebugIni must
	// recognise the directory, remove it, and write the real file.
	setupConfigHome(t)
	writeConfigYAML(t, "8.4", true)

	path := config.PHPConfFile("8.4")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureXdebugIni("8.4"); err != nil {
		t.Fatalf("EnsureXdebugIni: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("path missing after heal: %v", err)
	}
	if info.IsDir() {
		t.Errorf("path is still a directory, heal failed")
	}
	body, _ := os.ReadFile(path)
	// xdebug.mode=debug confirms it picked up the cfg.IsXdebugEnabled=true.
	if !strings.Contains(string(body), "xdebug.mode=debug") {
		t.Errorf("expected xdebug.mode=debug from config, got:\n%s", body)
	}
}

// ── WriteXdebugIni ───────────────────────────────────────────────────────────

func TestWriteXdebugIni_healsStaleDirectoryDirectly(t *testing.T) {
	// Even when called directly (lerd xdebug on/off after a broken
	// install), WriteXdebugIni must heal a stale directory rather than
	// fail with "is a directory" — otherwise users who hit the original
	// bug have no recovery path short of manually rmdir'ing the file.
	setupConfigHome(t)

	path := config.PHPConfFile("8.4")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}

	if err := WriteXdebugIni("8.4", true); err != nil {
		t.Fatalf("WriteXdebugIni: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("path missing: %v", err)
	}
	if info.IsDir() {
		t.Errorf("path still a directory after WriteXdebugIni")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "xdebug.mode=debug") {
		t.Errorf("expected xdebug.mode=debug, got:\n%s", body)
	}
}

// ── EnsureUserIni (same race surface as EnsureXdebugIni) ─────────────────────

func TestEnsureUserIni_createsWhenMissing(t *testing.T) {
	setupConfigHome(t)
	if err := EnsureUserIni("8.4"); err != nil {
		t.Fatalf("EnsureUserIni: %v", err)
	}
	info, err := os.Stat(config.PHPUserIniFile("8.4"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.IsDir() {
		t.Errorf("expected regular file, got directory")
	}
}

func TestEnsureUserIni_noopWhenRegularFileExists(t *testing.T) {
	// User php.ini files are explicitly meant to be hand-edited (per the
	// header comment in the default content). EnsureUserIni must never
	// stomp the file once it's a regular file on disk.
	setupConfigHome(t)
	path := config.PHPUserIniFile("8.4")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	custom := []byte("memory_limit = 1G\n")
	if err := os.WriteFile(path, custom, 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureUserIni("8.4"); err != nil {
		t.Fatalf("EnsureUserIni: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(custom) {
		t.Errorf("user ini was rewritten:\ngot:  %s\nwant: %s", got, custom)
	}
}

// ── ensureFPMHostsFile (third bind-mount source on the FPM quadlet) ──────────

func TestEnsureFPMHostsFile_writesWhenMissing(t *testing.T) {
	// First-install case: the shared /etc/hosts hasn't been written yet
	// but the FPM container is about to start. Without pre-creation
	// podman would auto-create the path as a directory.
	setupConfigHome(t)
	if err := ensureFPMHostsFile(); err != nil {
		t.Fatalf("ensureFPMHostsFile: %v", err)
	}
	info, err := os.Stat(config.ContainerHostsFile())
	if err != nil {
		t.Fatalf("hosts file not created: %v", err)
	}
	if info.IsDir() {
		t.Errorf("expected file, got directory")
	}
	body, _ := os.ReadFile(config.ContainerHostsFile())
	if !strings.Contains(string(body), "host.containers.internal") {
		t.Errorf("expected host.containers.internal in hosts file:\n%s", body)
	}
}

func TestEnsureFPMHostsFile_noopWhenRegularFileExists(t *testing.T) {
	// The file gets rewritten by WriteContainerHosts on every site
	// link/unlink/start, but the FPM-quadlet pre-create must NOT
	// rewrite it. Otherwise it would race with the watcher's reprobe
	// (which writes a verified host IP) and replace it with the
	// fallback header, breaking Xdebug temporarily.
	setupConfigHome(t)
	hostsPath := config.ContainerHostsFile()
	if err := os.MkdirAll(filepath.Dir(hostsPath), 0755); err != nil {
		t.Fatal(err)
	}
	custom := []byte("# user-managed hosts file\n10.0.0.5 host.containers.internal\n")
	if err := os.WriteFile(hostsPath, custom, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureFPMHostsFile(); err != nil {
		t.Fatalf("ensureFPMHostsFile: %v", err)
	}
	got, _ := os.ReadFile(hostsPath)
	if string(got) != string(custom) {
		t.Errorf("hosts file was rewritten\ngot:  %s\nwant: %s", got, custom)
	}
}

func TestEnsureFPMHostsFile_healsStaleDirectory(t *testing.T) {
	// Same race surface as the xdebug ini fix: podman left a directory
	// at the bind-mount source. ensureFPMHostsFile must remove it
	// before writing the real file or the next FPM start fails the
	// same way (and Xdebug continues to time out invisibly).
	setupConfigHome(t)
	hostsPath := config.ContainerHostsFile()
	if err := os.MkdirAll(hostsPath, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ensureFPMHostsFile(); err != nil {
		t.Fatalf("ensureFPMHostsFile: %v", err)
	}
	info, err := os.Stat(hostsPath)
	if err != nil {
		t.Fatalf("path missing after heal: %v", err)
	}
	if info.IsDir() {
		t.Errorf("path is still a directory, heal failed")
	}
}

func TestEnsureUserIni_healsStaleDirectory(t *testing.T) {
	// Same race as the original xdebug bug, on the user-ini bind-mount
	// declared in lerd-php-fpm.container.tmpl. EnsureUserIni currently
	// only checks whether the path exists, not whether it's a regular
	// file, so a podman-auto-created directory passes the check and the
	// real ini is never written. This test fails until EnsureUserIni
	// learns the same heal-stale-directory dance as EnsureXdebugIni.
	setupConfigHome(t)
	path := config.PHPUserIniFile("8.4")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureUserIni("8.4"); err != nil {
		t.Fatalf("EnsureUserIni: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("path missing after heal: %v", err)
	}
	if info.IsDir() {
		t.Errorf("path is still a directory, heal failed")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "PHP "+"8.4") {
		t.Errorf("expected default user ini content, got:\n%s", body)
	}
}
