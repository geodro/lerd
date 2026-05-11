//go:build linux

package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	nodeDet "github.com/geodro/lerd/internal/node"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
)

// removeWorkerExecArtifacts is a no-op on Linux: workers run as systemd
// service units that exec directly into podman, no guard script or pid
// file is written. The macOS build cleans up the script + pid file it
// generates alongside the plist.
func removeWorkerExecArtifacts(_ string) {}

// writeWorkerUnitFile writes a systemd service unit for the worker on Linux.
// Workers exec into the running FPM container.
//
// When schedule is non-empty the worker is modelled as a Type=oneshot
// service triggered by a sibling .timer with the given OnCalendar
// expression — the right shape for one-shot commands like Laravel 10's
// `php artisan schedule:run`, which exit immediately and would otherwise
// restart-loop every 5s under Restart=always.
func writeWorkerUnitFile(unitName, label, siteName, sitePath, phpVersion, command, restart, schedule, fpmUnit string, host bool) (bool, error) {
	if host {
		return writeHostWorkerUnitFile(unitName, label, siteName, sitePath, command, restart)
	}
	container := fpmUnit

	if schedule != "" {
		serviceUnit := fmt.Sprintf(`[Unit]
Description=Lerd %s (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=oneshot
ExecStart=%s exec -w %s %s %s
`, label, siteName, fpmUnit, fpmUnit, podman.PodmanBin(), sitePath, container, command)

		timerUnit := fmt.Sprintf(`[Unit]
Description=Lerd %s timer (%s)

[Timer]
OnCalendar=%s
Persistent=true
AccuracySec=1s

[Install]
WantedBy=timers.target
`, label, siteName, schedule)

		serviceChanged, err := services.Mgr.WriteServiceUnitIfChanged(unitName, serviceUnit)
		if err != nil {
			return false, err
		}
		timerChanged, err := services.Mgr.WriteTimerUnitIfChanged(unitName, timerUnit)
		if err != nil {
			return serviceChanged, err
		}
		return serviceChanged || timerChanged, nil
	}

	unit := fmt.Sprintf(`[Unit]
Description=Lerd %s (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=simple
Restart=%s
RestartSec=5
SuccessExitStatus=1 130 143
ExecStart=%s exec -w %s %s %s

[Install]
WantedBy=default.target
`, label, siteName, fpmUnit, fpmUnit, restart, podman.PodmanBin(), sitePath, container, command)

	// A previous run may have written a sibling .timer for this unit
	// (e.g. before the framework yaml dropped its `schedule:` field).
	// Clean it up so the daemon model takes over cleanly.
	_ = services.Mgr.RemoveTimerUnit(unitName)

	return services.Mgr.WriteServiceUnitIfChanged(unitName, unit)
}

const defaultNodeVersion = "22"

// writeHostWorkerUnitFile writes a systemd service unit for a worker that runs
// on the host via fnm rather than inside a container. Used for Node.js tools
// like Vite that need direct host access for HMR.
func writeHostWorkerUnitFile(unitName, label, siteName, sitePath, command, restart string) (bool, error) {
	fnm := filepath.Join(config.BinDir(), "fnm")
	nodeVersion, err := nodeDet.DetectVersion(sitePath)
	if err != nil {
		if cfg, _ := config.LoadGlobal(); cfg != nil {
			nodeVersion = cfg.Node.DefaultVersion
		}
		if nodeVersion == "" {
			nodeVersion = defaultNodeVersion
		}
	}

	// Wrap the framework worker command in /bin/sh -c so shell features
	// (&&, |, env-var expansion, redirects) work. systemd's ExecStart
	// performs argv-style splitting on whitespace and execve's the result
	// directly — without the wrapper, `npm run build && npm run preview`
	// passes "&&" to fnm as a literal argument and silently fails. Single
	// quotes inside the command are escaped via the standard '"'"' idiom
	// so the wrapper survives any user-provided string verbatim.
	shellCommand := fmt.Sprintf("%s exec --using=%s -- %s", fnm, nodeVersion, command)
	escaped := strings.ReplaceAll(shellCommand, "'", `'"'"'`)
	unit := fmt.Sprintf(`[Unit]
Description=Lerd %s (%s)

[Service]
Type=simple
Restart=%s
RestartSec=5
WorkingDirectory=%s
SuccessExitStatus=1 130 143
ExecStart=/bin/sh -c '%s'

[Install]
WantedBy=default.target
`, label, siteName, restart, sitePath, escaped)

	_ = services.Mgr.RemoveTimerUnit(unitName)
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

	fpmUnit := resolveWorkerFPMUnit(siteName, phpVersion)
	unitName, displaySite := workerNames(siteName, sitePath, workerName)

	restart := w.Restart
	if restart == "" {
		restart = "always"
	}
	label := w.Label
	if label == "" {
		label = workerName
	}

	changed, err := writeWorkerUnitFile(unitName, label, displaySite, sitePath, phpVersion, command, restart, w.Schedule, fpmUnit, w.Host)
	if err != nil {
		fmt.Printf("[WARN] writing worker unit %s: %v\n", unitName, err)
		return
	}
	if changed {
		enableTarget := unitName
		if w.Schedule != "" {
			enableTarget = unitName + ".timer"
		}
		if err := services.Mgr.Enable(enableTarget); err != nil {
			fmt.Printf("[WARN] enable %s: %v\n", enableTarget, err)
		}
	}
}
