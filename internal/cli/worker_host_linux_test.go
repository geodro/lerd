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
		"lerd-vite-mysite", "Vite", "mysite",
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
	// Boot ordering: host tools like Vite run wayfinder (php artisan) at
	// startup and crash if the FPM container isn't up yet. After+Wants
	// orders Vite behind FPM and pulls it up, without BindsTo's teardown.
	if !strings.Contains(unit, "After=network.target lerd-php84-fpm.service") {
		t.Errorf("host worker must order after the FPM unit; got:\n%s", unit)
	}
	if !strings.Contains(unit, "Wants=lerd-php84-fpm.service") {
		t.Errorf("host worker must pull up the FPM unit at boot; got:\n%s", unit)
	}
}

// A host-proxy site in a non-Node language (no package.json / .nvmrc /
// .node-version) must run its command directly, not through fnm, so it doesn't
// depend on Node being installed.
func TestWriteHostWorkerUnitFile_nonNodeRunsDirectly(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	binDir := filepath.Join(tmp, "lerd", "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "fnm"), []byte("#!/bin/sh"), 0755)

	// A Python project: no Node markers at all.
	sitePath := t.TempDir()
	os.WriteFile(filepath.Join(sitePath, "manage.py"), []byte("# django"), 0644)

	if _, err := writeWorkerUnitFile(
		"lerd-app-pysite", "Dev Server", "pysite",
		sitePath, "", "python manage.py runserver 0.0.0.0:8000",
		"always", "", "", true,
	); err != nil {
		t.Fatalf("writeWorkerUnitFile (host): %v", err)
	}

	unit, err := os.ReadFile(filepath.Join(tmp, "systemd", "user", "lerd-app-pysite.service"))
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	s := string(unit)
	if strings.Contains(s, "fnm") {
		t.Errorf("non-Node host worker must not route through fnm:\n%s", s)
	}
	if !strings.Contains(s, "python manage.py runserver") {
		t.Errorf("command missing from unit:\n%s", s)
	}
	if !strings.Contains(s, "/bin/sh -c") {
		t.Errorf("command should still be wrapped in /bin/sh -c:\n%s", s)
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

// TestWriteHostWorkerUnitFile_shellCommandPreserved pins the fix for the
// raw-ExecStart bug: framework worker commands containing shell
// metacharacters (&&, |, env-var expansion, redirects) must be wrapped in
// /bin/sh -c so systemd's argv-style splitting doesn't pass them as
// literal arguments to fnm. Without the wrap, "npm run build && npm run
// preview" would invoke fnm with "&&" as an argument and silently fail.
func TestWriteHostWorkerUnitFile_shellCommandPreserved(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	binDir := filepath.Join(tmp, "lerd", "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "fnm"), []byte("#!/bin/sh"), 0755)

	sitePath := t.TempDir()
	os.WriteFile(filepath.Join(sitePath, ".node-version"), []byte("20"), 0644)

	cases := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "simple command works as before",
			command: "npm run dev",
			want:    "npm run dev",
		},
		{
			name:    "command with && passes through to shell",
			command: "npm run build && npm run preview",
			want:    "npm run build && npm run preview",
		},
		{
			name:    "command with pipe",
			command: "tail -f log | grep error",
			want:    "tail -f log | grep error",
		},
		{
			name:    "command with single quote escapes safely",
			command: "echo 'hello world'",
			want:    "echo",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			os.RemoveAll(filepath.Join(tmp, "systemd"))
			_, err := writeWorkerUnitFile(
				"lerd-vite-shellcase", "Test", "shellcase",
				sitePath, "8.4", c.command,
				"on-failure", "", "lerd-php84-fpm", true,
			)
			if err != nil {
				t.Fatalf("writeWorkerUnitFile: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(tmp, "systemd", "user", "lerd-vite-shellcase.service"))
			if err != nil {
				t.Fatalf("read unit: %v", err)
			}
			unit := string(data)
			// ExecStart must invoke /bin/sh -c with the raw command so
			// shell metacharacters work. Verify the wrapper presence
			// and that the command substring survives.
			if !strings.Contains(unit, "/bin/sh -c") && !strings.Contains(unit, "/bin/sh' -c") {
				t.Errorf("ExecStart should wrap in /bin/sh -c for shell parsing; got:\n%s", unit)
			}
			if !strings.Contains(unit, c.want) {
				t.Errorf("command substring %q missing from unit:\n%s", c.want, unit)
			}
		})
	}
}

// Vite's Inertia/Wayfinder plugin shells out to `php artisan` from
// inside `npm run dev`. lerd's BinDir holds the php shim, so the host
// worker's PATH must lead with BinDir — issue #375.
func TestWriteHostWorkerUnitFile_pathLeadsWithLerdBinDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	binDir := filepath.Join(tmp, "lerd", "bin")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "fnm"), []byte("#!/bin/sh"), 0755)

	sitePath := t.TempDir()
	os.WriteFile(filepath.Join(sitePath, ".node-version"), []byte("20"), 0644)

	_, err := writeWorkerUnitFile(
		"lerd-vite-mysite", "Vite", "mysite",
		sitePath, "8.4", "npm run dev",
		"on-failure", "", "lerd-php84-fpm", true,
	)
	if err != nil {
		t.Fatalf("writeWorkerUnitFile: %v", err)
	}

	unitPath := filepath.Join(tmp, "systemd", "user", "lerd-vite-mysite.service")
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	unit := string(data)
	want := "Environment=PATH=" + config.BinDir() + ":"
	if !strings.Contains(unit, want) {
		t.Errorf("host worker unit must prepend lerd BinDir to PATH; got:\n%s", unit)
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
		Label:   "Vite",
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
