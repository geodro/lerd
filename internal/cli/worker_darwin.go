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
//
// Scheduled workers (Schedule != "") are not yet supported on macOS — the
// launchd StartCalendarInterval path needs its own plist generator. We log
// a warning and skip rather than restart-loop a one-shot command.
func writeWorkerUnitFile(unitName, label, siteName, sitePath, phpVersion, command, restart, schedule, fpmUnit string) (bool, error) {
	if schedule != "" {
		fmt.Printf("[WARN] worker %s has schedule=%q which is not yet supported on macOS — skipping\n", unitName, schedule)
		return false, nil
	}

	// Custom container sites exec into their own container.
	var unit string
	if site, _ := config.FindSite(siteName); site != nil && site.IsCustomContainer() {
		image := podman.CustomImageName(siteName)
		home, _ := os.UserHomeDir()
		unit = fmt.Sprintf(`[Container]
Image=%s
ContainerName=%s
Network=lerd
Volume=%s/.local/share/lerd/hosts:/etc/hosts:ro
Volume=%s:%s:rw
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
			sitePath,
			command,
			restart,
		)
	} else {
		versionShort := strings.ReplaceAll(phpVersion, ".", "")
		image := "lerd-php" + versionShort + "-fpm:local"
		home, _ := os.UserHomeDir()
		unit = fmt.Sprintf(`[Container]
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
	}

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

// restoreWorker is called from restoreSiteInfrastructure during `lerd start`.
// On macOS we only write the unit file; the actual start is deferred to
// phase 2 of runStart so we don't saturate the Podman Machine SSH connection
// before containers are ready.
func restoreWorker(siteName, sitePath, phpVersion, workerName string, w config.FrameworkWorker) {
	var fpmUnit string
	if site, _ := config.FindSite(siteName); site != nil && site.IsCustomContainer() {
		fpmUnit = podman.CustomContainerName(siteName)
	} else {
		versionShort := strings.ReplaceAll(phpVersion, ".", "")
		fpmUnit = "lerd-php" + versionShort + "-fpm"
	}
	unitName := "lerd-" + workerName + "-" + siteName
	restart := w.Restart
	if restart == "" {
		restart = "always"
	}
	label := w.Label
	if label == "" {
		label = workerName
	}
	writeWorkerUnitFile(unitName, label, siteName, sitePath, phpVersion, w.Command, restart, w.Schedule, fpmUnit) //nolint:errcheck
}
