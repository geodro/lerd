//go:build linux

package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
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

// restoreWorker is called from restoreSiteInfrastructure during `lerd start`,
// before phase 1 brings up containers. We only write the unit file and enable
// it; the actual Start happens in phase 2 of runStart once lerd-redis and the
// other infra containers are up. Starting here would race against container
// readiness and cause errors like "lerd-redis: name does not resolve".
func restoreWorker(siteName, sitePath, phpVersion, workerName string, w config.FrameworkWorker) {
	command := w.Command
	if w.Proxy != nil && w.Proxy.PortEnvKey != "" {
		envPath := filepath.Join(sitePath, ".env")
		port := envfile.ReadKey(envPath, w.Proxy.PortEnvKey)
		if port == "" {
			port = strconv.Itoa(assignWorkerProxyPort(sitePath, w.Proxy.PortEnvKey, w.Proxy.DefaultPort))
			_ = envfile.ApplyUpdates(envPath, map[string]string{w.Proxy.PortEnvKey: port})
		}
		command = command + " --port=" + port
	}

	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	fpmUnit := "lerd-php" + versionShort + "-fpm"
	unitName := "lerd-" + workerName + "-" + siteName

	restart := w.Restart
	if restart == "" {
		restart = "always"
	}
	label := w.Label
	if label == "" {
		label = workerName
	}

	changed, err := writeWorkerUnitFile(unitName, label, siteName, sitePath, phpVersion, command, restart, fpmUnit)
	if err != nil {
		fmt.Printf("[WARN] writing worker unit %s: %v\n", unitName, err)
		return
	}
	if changed {
		if err := services.Mgr.Enable(unitName); err != nil {
			fmt.Printf("[WARN] enable %s: %v\n", unitName, err)
		}
	}
}
