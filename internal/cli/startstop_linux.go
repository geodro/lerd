//go:build linux

package cli

// ensurePodmanMachineRunning is a no-op on Linux — Podman runs natively
// without a VM, so no machine needs to be started before running containers.
func ensurePodmanMachineRunning() {}

// migrateExecWorkerPlists is a no-op on Linux — exec-based plists only existed
// in the macOS-specific alpha.2/alpha.3 launchd plist implementation.
func migrateExecWorkerPlists() {}

// batchStopContainers is a no-op on Linux — systemd stops containers via unit
// deactivation so individual StopUnit calls are efficient and non-blocking.
func batchStopContainers(_ []string) {}

// stopPodmanMachine is a no-op on Linux — Podman runs natively without a VM.
func stopPodmanMachine() {}
