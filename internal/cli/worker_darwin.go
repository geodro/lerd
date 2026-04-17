//go:build darwin

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
)

// writeWorkerUnitFile writes the macOS launch artifacts for a framework
// worker. Which shape we produce depends on cfg.WorkerExecMode():
//
//   - "exec" (default): a service unit whose ExecStart runs a generated
//     guard script that `podman exec`s into the shared FPM container.
//     launchd supervises the outer process; the guard prevents duplicate
//     workers when the podman-machine SSH bridge hiccups.
//   - "container": one detached container per worker, spawned from the
//     FPM image. Higher memory but 1:1 supervisor boundary.
//
// Scheduled workers (Schedule != "") still aren't supported on macOS.
func writeWorkerUnitFile(unitName, label, siteName, sitePath, phpVersion, command, restart, schedule, fpmUnit string) (bool, error) {
	if schedule != "" {
		fmt.Printf("[WARN] worker %s has schedule=%q which is not yet supported on macOS — skipping\n", unitName, schedule)
		return false, nil
	}

	cfg, _ := config.LoadGlobal()
	mode := config.WorkerExecModeExec
	if cfg != nil {
		mode = cfg.WorkerExecMode()
	}

	switch mode {
	case config.WorkerExecModeContainer:
		return writeWorkerContainerUnit(unitName, siteName, sitePath, phpVersion, command, restart)
	default:
		return writeWorkerExecUnit(unitName, siteName, sitePath, phpVersion, command, restart, fpmUnit)
	}
}

// writeWorkerExecUnit is the `exec` macOS path: write a guard script and a
// service unit whose ExecStart invokes it. services.Mgr translates the
// service unit into a launchd plist.
func writeWorkerExecUnit(unitName, siteName, sitePath, phpVersion, command, restart, fpmUnit string) (bool, error) {
	workersDir := filepath.Join(config.RunDir(), "workers")
	if err := os.MkdirAll(workersDir, 0755); err != nil {
		return false, fmt.Errorf("creating worker run dir: %w", err)
	}
	scriptPath := filepath.Join(workersDir, unitName+".sh")
	pidFile := filepath.Join(workersDir, unitName+".pid")

	// Resolve the container to exec into: custom site → its own container,
	// PHP site → shared FPM for that version.
	var container string
	if site, _ := config.FindSite(siteName); site != nil && site.IsCustomContainer() {
		container = podman.CustomContainerName(siteName)
	} else {
		versionShort := strings.ReplaceAll(phpVersion, ".", "")
		container = "lerd-php" + versionShort + "-fpm"
		_ = fpmUnit // kept for API parity with the Linux implementation
	}

	podmanExec := fmt.Sprintf("%s exec -w %s %s %s", podman.PodmanBin(), sitePath, container, command)
	script := buildDarwinExecWorkerGuardScript(pidFile, podmanExec)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return false, fmt.Errorf("writing worker guard script: %w", err)
	}

	unit := buildDarwinExecWorkerService(scriptPath, restart)
	if err := services.Mgr.WriteServiceUnit(unitName, unit); err != nil {
		return false, err
	}
	if err := podman.DaemonReloadFn(); err != nil {
		return false, err
	}
	return true, nil
}

// writeWorkerContainerUnit is the original `container` macOS path: one
// detached container per worker, spawned from the FPM image.
func writeWorkerContainerUnit(unitName, siteName, sitePath, phpVersion, command, restart string) (bool, error) {
	home, _ := os.UserHomeDir()

	var unit string
	if site, _ := config.FindSite(siteName); site != nil && site.IsCustomContainer() {
		// Build the custom-container unit and substitute the placeholder
		// image name the builder emits.
		unit = buildDarwinContainerWorkerUnit(unitName, "", sitePath, home, "", "", command, restart, true)
		unit = strings.Replace(unit, "<custom-image>", podman.CustomImageName(siteName), 1)
	} else {
		unit = buildDarwinContainerWorkerUnit(
			unitName, phpVersion, sitePath, home,
			config.PHPConfFile(phpVersion), config.PHPUserIniFile(phpVersion),
			command, restart, false,
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
// The hint is the same for both modes: in exec mode the `podman logs`
// target is the shared FPM container (worker output is interleaved with
// other exec traffic), in container mode it's the dedicated worker
// container. We use the log_tail.go path to route the TUI correctly.
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
