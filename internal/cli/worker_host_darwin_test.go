//go:build darwin

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/services"
)

// trackingHostMgr captures the (name, content) WriteServiceUnit was
// called with so writeWorkerHostUnit's wiring through services.Mgr can
// be asserted without going through the real launchd translator.
type trackingHostMgr struct {
	stopTrackingMgr
	mu       sync.Mutex
	unitName string
	unitBody string
	called   bool
}

func (m *trackingHostMgr) WriteServiceUnit(name, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unitName = name
	m.unitBody = content
	m.called = true
	return nil
}

// withTempXDGAndBin redirects XDG_DATA_HOME so config.RunDir /
// config.BinDir resolve under a throwaway tempdir, and seeds empty `fnm`
// and `node` binaries at config.BinDir() so resolveNodeVersionForHostWorker
// can find fnm and lerdManagesNode() reports true (the host worker only
// pins via fnm when lerd manages Node). Returns the tempdir for assertions.
func withTempXDGAndBin(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	_ = os.MkdirAll(config.BinDir(), 0755)
	_ = os.WriteFile(filepath.Join(config.BinDir(), "fnm"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	_ = os.WriteFile(filepath.Join(config.BinDir(), "node"), []byte("#!/bin/sh\nexit 0\n"), 0755)
	return tmp
}

// TestWriteWorkerHostUnit_writesGuardAndServiceUnit pins the end-to-end
// wiring of the host worker pipeline on macOS. The guard script lands
// at run/workers/<unit>.sh with the correct shebang, cd, and fnm exec
// line; services.Mgr.WriteServiceUnit is invoked with the matching
// systemd-style service body; DaemonReloadFn fires once.
func TestWriteWorkerHostUnit_writesGuardAndServiceUnit(t *testing.T) {
	tmp := withTempXDGAndBin(t)
	mgr := &trackingHostMgr{}
	swapMgr(t, mgr)
	reloadCalls := swapDaemonReload(t)

	// Fake a node version pin so resolveNodeVersionForHostWorker is
	// deterministic without depending on the real detector. The
	// detector extracts the major from .nvmrc (22.16.0 → "22"), so
	// that's what should land in the guard script.
	sitePath := filepath.Join(tmp, "site")
	if err := os.MkdirAll(sitePath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sitePath, ".nvmrc"), []byte("22.16.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	changed, err := writeWorkerHostUnit("lerd-vite-mysite", sitePath, "npm run dev", "always")
	if err != nil {
		t.Fatalf("writeWorkerHostUnit: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true on first write")
	}

	// Guard script written + readable + correctly shaped.
	scriptPath := filepath.Join(config.RunDir(), "workers", "lerd-vite-mysite.sh")
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("reading guard script: %v", err)
	}
	bodyStr := string(body)
	for _, want := range []string{
		"#!/bin/sh",
		"cd '" + sitePath + "'",
		filepath.Join(config.BinDir(), "fnm"),
		"exec --using=22",
		"-- /bin/sh -c 'npm run dev'",
	} {
		if !strings.Contains(bodyStr, want) {
			t.Errorf("guard script missing %q:\n%s", want, bodyStr)
		}
	}

	// Script must be executable (launchd's /bin/sh ExecStart needs it).
	info, _ := os.Stat(scriptPath)
	if info.Mode()&0111 == 0 {
		t.Errorf("guard script not executable: %v", info.Mode())
	}

	// services.Mgr.WriteServiceUnit was invoked with the unit name and a
	// systemd-style body whose ExecStart points at our guard script.
	if !mgr.called {
		t.Fatalf("services.Mgr.WriteServiceUnit was not called")
	}
	if mgr.unitName != "lerd-vite-mysite" {
		t.Errorf("unit name = %q, want lerd-vite-mysite", mgr.unitName)
	}
	if !strings.Contains(mgr.unitBody, "ExecStart=/bin/sh "+scriptPath) {
		t.Errorf("service unit ExecStart missing or wrong:\n%s", mgr.unitBody)
	}
	if !strings.Contains(mgr.unitBody, "Restart=always") {
		t.Errorf("service unit Restart not threaded through:\n%s", mgr.unitBody)
	}

	if *reloadCalls != 1 {
		t.Errorf("DaemonReloadFn called %d times, want 1", *reloadCalls)
	}
}

// TestWriteWorkerHostUnit_servicesMgrErrorPropagates pins that a
// WriteServiceUnit failure surfaces — without this, the caller would
// think the unit is in place but launchd has nothing to start, and
// the watcher would heal-loop the missing plist forever.
func TestWriteWorkerHostUnit_servicesMgrErrorPropagates(t *testing.T) {
	withTempXDGAndBin(t)
	mgr := &errorMgr{writeErr: errFake}
	swapMgr(t, mgr)
	swapDaemonReload(t)

	_, err := writeWorkerHostUnit("lerd-vite-mysite", "/site", "npm run dev", "always")
	if err == nil {
		t.Fatalf("expected error from services.Mgr.WriteServiceUnit; got nil")
	}
}

// TestResolveNodeVersionForHostWorker_cascade pins the version
// resolution order: detector → config.Node.DefaultVersion → fallback.
func TestResolveNodeVersionForHostWorker_cascade(t *testing.T) {
	t.Run("detector wins when .nvmrc exists", func(t *testing.T) {
		tmp := withTempXDGAndBin(t)
		site := filepath.Join(tmp, "site")
		_ = os.MkdirAll(site, 0755)
		_ = os.WriteFile(filepath.Join(site, ".nvmrc"), []byte("20.11.0\n"), 0644)

		// Detector extracts the major from .nvmrc semver strings, so
		// "20.11.0" becomes "20". The host-worker resolver returns
		// whatever the detector emits.
		if got := resolveNodeVersionForHostWorker(site); got != "20" {
			t.Errorf("got %q, want 20", got)
		}
	})

	t.Run("config default when no project hint", func(t *testing.T) {
		tmp := withTempXDGAndBin(t)
		site := filepath.Join(tmp, "site")
		_ = os.MkdirAll(site, 0755)
		// No .nvmrc, no package.json engines.

		cfg, _ := config.LoadGlobal()
		cfg.Node.DefaultVersion = "21"
		if err := config.SaveGlobal(cfg); err != nil {
			t.Fatal(err)
		}

		if got := resolveNodeVersionForHostWorker(site); got != "21" {
			t.Errorf("got %q, want 21 (cfg.Node.DefaultVersion)", got)
		}
	})

	t.Run("hardcoded fallback when nothing else is set", func(t *testing.T) {
		tmp := withTempXDGAndBin(t)
		site := filepath.Join(tmp, "site")
		_ = os.MkdirAll(site, 0755)
		// cfg.Node.DefaultVersion left empty.
		cfg, _ := config.LoadGlobal()
		cfg.Node.DefaultVersion = ""
		_ = config.SaveGlobal(cfg)

		if got := resolveNodeVersionForHostWorker(site); got != defaultMacOSNodeVersion {
			t.Errorf("got %q, want %q", got, defaultMacOSNodeVersion)
		}
	})
}

// errorMgr forces WriteServiceUnit to fail.
type errorMgr struct {
	stopTrackingMgr
	writeErr error
}

func (m *errorMgr) WriteServiceUnit(string, string) error { return m.writeErr }

var errFake = errFakeWriter("fake-write-failure")

type errFakeWriter string

func (e errFakeWriter) Error() string { return string(e) }

// silence linter — services package import is reachable through swapMgr.
var _ services.ServiceManager = (*trackingHostMgr)(nil)
