//go:build linux

package cli

// currentMachineLastUp is a no-op on Linux. Podman runs natively without
// a VM, so there is no LastUp to read and no gvproxy to lose host-side
// port forwards.
func currentMachineLastUp() string { return "" }

// healMachineRestartIfNeeded is a no-op on Linux. See
// currentMachineLastUp for the rationale.
func healMachineRestartIfNeeded(_ string) {}
