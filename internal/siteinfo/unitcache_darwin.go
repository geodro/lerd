//go:build darwin

package siteinfo

import "github.com/geodro/lerd/internal/podman"

func init() {
	// On macOS, workers are managed by launchd + podman containers — there is
	// no systemd. Override the default unitStatusFn (which calls systemctl) so
	// that worker status is queried through the darwinServiceManager instead,
	// which checks launchd state and the running podman container directly.
	unitStatusFn = podman.UnitStatus
}
