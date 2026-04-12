//go:build linux

package cli

import (
	"fmt"
	"strings"

	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
)

// writeWorkerUnitFile writes a systemd service unit for the worker on Linux.
// Workers exec into the running FPM container.
func writeWorkerUnitFile(unitName, label, siteName, sitePath, phpVersion, command, restart, fpmUnit string) (bool, error) {
	container := fpmUnit
	unit := fmt.Sprintf(`[Unit]
Description=Lerd %s (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=%s
RestartSec=5
ExecStart=%s exec -w %s %s %s

[Install]
WantedBy=default.target
`, label, siteName, fpmUnit, fpmUnit, restart, podman.PodmanBin(), sitePath, container, command)

	return services.Mgr.WriteServiceUnitIfChanged(unitName, unit)
}

// workerLogHint returns the hint for viewing worker logs on Linux.
func workerLogHint(unitName string) string {
	return "journalctl --user -u " + unitName + " -f"
}

