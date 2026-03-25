//go:build darwin

package services

import "errors"

func init() {
	Mgr = &darwinServiceManager{}
}

type darwinServiceManager struct{}

var errNotImplemented = errors.New("not implemented on macOS yet")

func (m *darwinServiceManager) WriteServiceUnit(_, _ string) error        { return errNotImplemented }
func (m *darwinServiceManager) RemoveServiceUnit(_ string) error           { return errNotImplemented }
func (m *darwinServiceManager) ListServiceUnits(_ string) []string         { return nil }
func (m *darwinServiceManager) WriteContainerUnit(_, _ string) error       { return errNotImplemented }
func (m *darwinServiceManager) ContainerUnitInstalled(_ string) bool       { return false }
func (m *darwinServiceManager) RemoveContainerUnit(_ string) error         { return errNotImplemented }
func (m *darwinServiceManager) ListContainerUnits(_ string) []string       { return nil }
func (m *darwinServiceManager) DaemonReload() error                        { return errNotImplemented }
func (m *darwinServiceManager) Start(_ string) error                       { return errNotImplemented }
func (m *darwinServiceManager) Stop(_ string) error                        { return errNotImplemented }
func (m *darwinServiceManager) Restart(_ string) error                     { return errNotImplemented }
func (m *darwinServiceManager) Enable(_ string) error                      { return errNotImplemented }
func (m *darwinServiceManager) Disable(_ string) error                     { return errNotImplemented }
func (m *darwinServiceManager) IsActive(_ string) bool                     { return false }
func (m *darwinServiceManager) IsEnabled(_ string) bool                    { return false }
func (m *darwinServiceManager) UnitStatus(_ string) (string, error)        { return "unknown", nil }

func (m *darwinServiceManager) WriteServiceUnitIfChanged(_, _ string) (bool, error) {
	return false, errNotImplemented
}
