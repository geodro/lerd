//go:build linux

package services

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
)

func init() {
	Mgr = &linuxServiceManager{}
}

type linuxServiceManager struct{}

// --- Service unit files ---

func (m *linuxServiceManager) WriteServiceUnit(name, content string) error {
	return lerdSystemd.WriteService(name, content)
}

func (m *linuxServiceManager) WriteServiceUnitIfChanged(name, content string) (bool, error) {
	return lerdSystemd.WriteServiceIfChanged(name, content)
}

func (m *linuxServiceManager) WriteTimerUnitIfChanged(name, content string) (bool, error) {
	return lerdSystemd.WriteTimerIfChanged(name, content)
}

func (m *linuxServiceManager) RemoveTimerUnit(name string) error {
	return lerdSystemd.RemoveTimer(name)
}

func (m *linuxServiceManager) ListTimerUnits(nameGlob string) []string {
	pattern := filepath.Join(config.SystemdUserDir(), nameGlob+".timer")
	entries, _ := filepath.Glob(pattern)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		// Keep the .timer suffix so callers can distinguish from
		// the sibling .service unit when starting/stopping.
		names = append(names, strings.TrimSuffix(filepath.Base(e), ".timer")+".timer")
	}
	return names
}

func (m *linuxServiceManager) RemoveServiceUnit(name string) error {
	path := filepath.Join(config.SystemdUserDir(), name+".service")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *linuxServiceManager) ListServiceUnits(nameGlob string) []string {
	pattern := filepath.Join(config.SystemdUserDir(), nameGlob+".service")
	entries, _ := filepath.Glob(pattern)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(filepath.Base(e), ".service"))
	}
	return names
}

// --- Container unit files ---

func (m *linuxServiceManager) WriteContainerUnit(name, content string) error {
	return podman.WriteQuadlet(name, content)
}

func (m *linuxServiceManager) ContainerUnitInstalled(name string) bool {
	return podman.QuadletInstalled(name)
}

func (m *linuxServiceManager) RemoveContainerUnit(name string) error {
	return podman.RemoveQuadlet(name)
}

func (m *linuxServiceManager) ListContainerUnits(nameGlob string) []string {
	pattern := filepath.Join(config.QuadletDir(), nameGlob+".container")
	entries, _ := filepath.Glob(pattern)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(filepath.Base(e), ".container"))
	}
	return names
}

// --- Service lifecycle ---

func (m *linuxServiceManager) DaemonReload() error {
	return podman.DaemonReload()
}

func (m *linuxServiceManager) Start(name string) error {
	return podman.StartUnit(name)
}

func (m *linuxServiceManager) Stop(name string) error {
	return podman.StopUnit(name)
}

func (m *linuxServiceManager) Restart(name string) error {
	return podman.RestartUnit(name)
}

func (m *linuxServiceManager) Enable(name string) error {
	return lerdSystemd.EnableService(name)
}

func (m *linuxServiceManager) Disable(name string) error {
	return lerdSystemd.DisableService(name)
}

func (m *linuxServiceManager) IsActive(name string) bool {
	return lerdSystemd.IsServiceActive(name)
}

func (m *linuxServiceManager) IsEnabled(name string) bool {
	return lerdSystemd.IsServiceEnabled(name)
}

func (m *linuxServiceManager) UnitStatus(name string) (string, error) {
	return podman.UnitStatus(name)
}
