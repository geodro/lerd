//go:build linux

package cli

// ensurePodmanMachineRunning is a no-op on Linux — Podman runs natively
// without a VM, so no machine needs to be started before running containers.
func ensurePodmanMachineRunning() {}
