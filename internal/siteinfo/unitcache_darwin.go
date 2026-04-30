//go:build darwin

package siteinfo

import "github.com/geodro/lerd/internal/podman"

func init() {
	// On macOS, workers are managed by launchd + podman containers — there is
	// no systemd. Override the default unitStatusFn (which calls systemctl) so
	// that worker status is queried through the darwinServiceManager instead,
	// which checks launchd state and the running podman container directly.
	unitStatusFn = podman.UnitStatus

	// AllUnitStates is the batched-state accessor used by the worker-health
	// detector. On macOS there is no systemctl batched-list equivalent (each
	// `launchctl print` is one process), so return empty rather than shell
	// out per call. The macOS exec-mode self-heal watcher already covers the
	// drift cases the Linux detector targets.
	unitCacheListFn = func() (string, error) { return "", nil }
}
