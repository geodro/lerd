//go:build darwin

package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
)

// writeWorkerUnitFile writes a container unit for the worker on macOS.
// Workers run as independent detached containers using the same PHP-FPM image,
// so each worker has its own lifecycle and logs stream via `podman logs`.
func writeWorkerUnitFile(unitName, label, siteName, sitePath, phpVersion, command, restart, fpmUnit string) (bool, error) {
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	image := "lerd-php" + versionShort + "-fpm:local"

	// Build a quadlet-style [Container] unit. On macOS, WriteContainerUnit
	// converts this to a launchd plist running `podman run -d --restart=always`.
	home, _ := os.UserHomeDir()
	unit := fmt.Sprintf(`[Container]
Image=%s
ContainerName=%s
Network=lerd
Volume=%s/.local/share/lerd/hosts:/etc/hosts:ro
Volume=%s:%s:rw
Volume=%s:/usr/local/etc/php/conf.d/99-xdebug.ini:ro
Volume=%s:/usr/local/etc/php/conf.d/98-lerd-user.ini:ro
PodmanArgs=--security-opt=label=disable
WorkingDir=%s
Exec=%s

[Service]
Restart=%s
`,
		image,
		unitName,
		home,
		sitePath, sitePath,
		config.PHPConfFile(phpVersion),
		config.PHPUserIniFile(phpVersion),
		sitePath,
		command,
		restart,
	)

	if err := services.Mgr.WriteContainerUnit(unitName, unit); err != nil {
		return false, err
	}
	if err := podman.DaemonReloadFn(); err != nil {
		return false, err
	}
	return true, nil
}

// workerLogHint returns the hint for viewing worker logs on macOS.
func workerLogHint(unitName string) string {
	return "podman logs -f " + unitName
}

