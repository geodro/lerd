//go:build linux

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestWriteHostWorkerUnitFile_useFnmExec(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	binDir := filepath.Join(tmp, "lerd", "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "fnm"), []byte("#!/bin/sh"), 0755)

	sitePath := t.TempDir()
	os.WriteFile(filepath.Join(sitePath, ".node-version"), []byte("20"), 0644)

	changed, err := writeWorkerUnitFile(
		"lerd-vite-mysite", "Vite Dev Server", "mysite",
		sitePath, "8.4", "npm run dev",
		"on-failure", "", "lerd-php84-fpm", true,
	)
	if err != nil {
		t.Fatalf("writeWorkerUnitFile (host): %v", err)
	}
	if !changed {
		t.Error("first write reported changed=false, want true")
	}

	unitPath := filepath.Join(tmp, "systemd", "user", "lerd-vite-mysite.service")
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	unit := string(data)

	if strings.Contains(unit, "podman") {
		t.Error("host worker must not use podman exec")
	}
	if !strings.Contains(unit, "fnm") {
		t.Error("host worker must use fnm exec")
	}
	if !strings.Contains(unit, "--using=20") {
		t.Errorf("expected --using=20 from .node-version, got:\n%s", unit)
	}
	if !strings.Contains(unit, "WorkingDirectory=") {
		t.Error("host worker must set WorkingDirectory")
	}
	if !strings.Contains(unit, "npm run dev") {
		t.Error("host worker must include the command")
	}
	if !strings.Contains(unit, "Restart=on-failure") {
		t.Error("host worker must respect restart policy")
	}
	if strings.Contains(unit, "BindsTo=") {
		t.Error("host worker must not bind to FPM container")
	}
}

func TestWriteWorkerUnitFile_hostFalse_usesPodman(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	changed, err := writeWorkerUnitFile(
		"lerd-horizon-mysite", "Horizon", "mysite",
		"/srv/mysite", "8.4", "php artisan horizon",
		"always", "", "lerd-php84-fpm", false,
	)
	if err != nil {
		t.Fatalf("writeWorkerUnitFile (container): %v", err)
	}
	if !changed {
		t.Error("first write reported changed=false")
	}

	unitPath := filepath.Join(tmp, "systemd", "user", "lerd-horizon-mysite.service")
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	unit := string(data)

	if !strings.Contains(unit, "podman") {
		t.Error("container worker must use podman exec")
	}
	if !strings.Contains(unit, "BindsTo=lerd-php84-fpm.service") {
		t.Error("container worker must bind to FPM unit")
	}
}

func TestWorkerStartForSite_worktreeUnitNaming(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// Register a site so FindSite works
	sitePath := t.TempDir()
	reg := &config.SiteRegistry{Sites: []config.Site{{Name: "mysite", Domains: []string{"mysite.test"}, Path: sitePath}}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatalf("SaveSites: %v", err)
	}

	// Create a worktree path different from site path
	wtPath := t.TempDir()
	os.WriteFile(filepath.Join(wtPath, ".node-version"), []byte("20"), 0644)

	binDir := filepath.Join(tmp, "lerd", "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "fnm"), []byte("#!/bin/sh"), 0755)

	w := config.FrameworkWorker{
		Label:   "Vite Dev Server",
		Command: "npm run dev",
		Restart: "on-failure",
		Host:    true,
	}

	err := WorkerStartForSite("mysite", wtPath, "8.4", "vite", w, false)
	// Will fail to start the unit (no real systemd), but unit file should be written
	// with per-worktree naming
	_ = err

	systemdDir := filepath.Join(tmp, "systemd", "user")
	entries, _ := os.ReadDir(systemdDir)
	var found bool
	for _, e := range entries {
		if strings.Contains(e.Name(), "lerd-vite-mysite-") && strings.HasSuffix(e.Name(), ".service") {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected per-worktree unit name lerd-vite-mysite-<dir>, got: %v", names)
	}
}
