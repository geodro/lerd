//go:build linux

package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestLinuxListServiceUnits(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := config.SystemdUserDir()
	os.MkdirAll(dir, 0755)

	os.WriteFile(filepath.Join(dir, "lerd-watcher.service"), []byte("[Service]\n"), 0644)
	os.WriteFile(filepath.Join(dir, "lerd-queue-myapp.service"), []byte("[Service]\n"), 0644)
	os.WriteFile(filepath.Join(dir, "other.service"), []byte("[Service]\n"), 0644)

	mgr := &linuxServiceManager{}
	units := mgr.ListServiceUnits("lerd-*")
	if len(units) != 2 {
		t.Errorf("expected 2 units matching lerd-*, got %d: %v", len(units), units)
	}

	found := map[string]bool{}
	for _, u := range units {
		found[u] = true
	}
	if !found["lerd-watcher"] || !found["lerd-queue-myapp"] {
		t.Errorf("missing expected units: %v", units)
	}
}

func TestLinuxListContainerUnits(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := config.QuadletDir()
	os.MkdirAll(dir, 0755)

	os.WriteFile(filepath.Join(dir, "lerd-nginx.container"), []byte("[Container]\n"), 0644)
	os.WriteFile(filepath.Join(dir, "lerd-dns.container"), []byte("[Container]\n"), 0644)

	mgr := &linuxServiceManager{}
	units := mgr.ListContainerUnits("lerd-*")
	if len(units) != 2 {
		t.Errorf("expected 2 container units, got %d: %v", len(units), units)
	}
}

func TestLinuxRemoveServiceUnit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := config.SystemdUserDir()
	os.MkdirAll(dir, 0755)

	path := filepath.Join(dir, "lerd-test.service")
	os.WriteFile(path, []byte("[Service]\n"), 0644)

	mgr := &linuxServiceManager{}
	if err := mgr.RemoveServiceUnit("lerd-test"); err != nil {
		t.Fatalf("RemoveServiceUnit: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("service unit file should be removed")
	}
}

func TestLinuxRemoveServiceUnitNotExists(t *testing.T) {
	mgr := &linuxServiceManager{}
	if err := mgr.RemoveServiceUnit("lerd-nonexistent"); err != nil {
		t.Fatalf("RemoveServiceUnit of nonexistent unit should not error: %v", err)
	}
}

func TestLinuxContainerUnitInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := config.QuadletDir()
	os.MkdirAll(dir, 0755)

	mgr := &linuxServiceManager{}
	if mgr.ContainerUnitInstalled("lerd-test") {
		t.Error("should not be installed")
	}

	os.WriteFile(filepath.Join(dir, "lerd-test.container"), []byte("[Container]\n"), 0644)
	if !mgr.ContainerUnitInstalled("lerd-test") {
		t.Error("should be installed after writing file")
	}
}
