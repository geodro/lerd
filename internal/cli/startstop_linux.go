//go:build linux

package cli

// ensurePodmanMachineRunning is a no-op on Linux — Podman runs natively
// without a VM, so no machine needs to be started before running containers.
func ensurePodmanMachineRunning() {}

// migrateExecWorkerPlists is a no-op on Linux — exec-based plists only existed
// in the macOS-specific alpha.2/alpha.3 launchd plist implementation.
func migrateExecWorkerPlists() {}
