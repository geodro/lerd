//go:build linux

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/geodro/lerd/internal/feedback"
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
	// Generation-boundary guard so every caller is covered (incl. the boot
	// restore path): a newline in the command would inject a second systemd
	// directive from a cloned repo's .lerd.yaml custom_workers entry.
	if config.ContainsUnitInjectionChars(command) {
		return false, fmt.Errorf("worker unit %q: command must not contain newline or NUL", unitName)
	}
	if host {
		return writeHostWorkerUnitFile(unitName, label, siteName, sitePath, command, restart, fpmUnit)
	}
	container := fpmUnit

	if schedule != "" {
		serviceUnit := fmt.Sprintf(`[Unit]
Description=Lerd %s (%s)
After=network.target %s.service
BindsTo=%s.service

[Service]
Type=oneshot
ExecStart=%s exec -w %s --env=LERD_SITE=%s %s %s
`, label, siteName, fpmUnit, fpmUnit, podman.PodmanBin(), sitePath, siteName, container, command)

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
ExecStart=%s exec -w %s --env=LERD_SITE=%s %s %s

[Install]
WantedBy=default.target
`, label, siteName, fpmUnit, fpmUnit, restart, podman.PodmanBin(), sitePath, siteName, container, command)

	// A previous run may have written a sibling .timer for this unit
	// (e.g. before the framework yaml dropped its `schedule:` field).
	// Clean it up so the daemon model takes over cleanly.
	_ = services.Mgr.RemoveTimerUnit(unitName)

	return services.Mgr.WriteServiceUnitIfChanged(unitName, unit)
}

const defaultNodeVersion = "22"

// writeHostWorkerUnitFile writes a systemd service unit for a worker that runs
// on the host rather than inside a container. Node projects run through fnm so
// the right Node version is used (Vite, Nest, etc.); a host-proxy site in any
// other language (Python, Ruby, Go, …) runs its command directly. When the
// worker backs a PHP site (fpmUnit set) the unit is ordered after and pulls up
// that FPM container; host-proxy sites have no FPM and omit the dependency.
func writeHostWorkerUnitFile(unitName, label, siteName, sitePath, command, restart, fpmUnit string) (bool, error) {
	// Wrap the command in /bin/sh -c so shell features (&&, |, env-var
	// expansion, redirects) work. systemd's ExecStart performs argv-style
	// splitting on whitespace and execve's the result directly, so without the
	// wrapper `npm run build && npm run preview` would pass "&&" as a literal
	// argument. Single quotes are escaped via the standard '"'"' idiom so the
	// wrapper survives any user-provided string verbatim.
	home, _ := os.UserHomeDir()
	// lerd's shim must lead PATH so wayfinder + friends find `php`; we
	// rebuild the path systemd's user default would have supplied so
	// `~/.local/bin` stays reachable — issue #375.
	envPath := config.BinDir() + ":" + filepath.Join(home, ".local", "bin") + ":/usr/local/bin:/usr/bin:/bin"

	shellCommand := command
	if bun := bunRunnerFor(sitePath, false); bun != "" {
		// bun is self-contained: rewrite npm/npx/node to bun/bunx and run it
		// directly, no fnm wrap. Put ~/.bun/bin on PATH so a bare `bun` resolves.
		shellCommand = nodeDet.Bunify(command)
		envPath = filepath.Dir(bun) + ":" + envPath
	} else if isNodeProject(sitePath) && lerdManagesNode() {
		// Only route through fnm when lerd is actually managing Node; otherwise
		// run the command directly so the user's system node/npm on PATH is used
		// (after node:unmanage there is no fnm Node to exec into).
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
		shellCommand = fmt.Sprintf("%s exec --using=%s -- %s", fnm, nodeVersion, command)
	}
	escaped := strings.ReplaceAll(shellCommand, "'", `'"'"'`)
	// Order after and pull up the site's FPM container: host tools like Vite
	// run wayfinder (php artisan) at startup, which fails if FPM isn't up yet
	// at boot. Wants, not BindsTo, so a transient FPM restart can't kill Vite.
	// Host-proxy sites in non-PHP languages have no FPM unit, so skip it.
	fpmOrder := ""
	if fpmUnit != "" {
		fpmOrder = fmt.Sprintf("After=network.target %s.service\nWants=%s.service\n", fpmUnit, fpmUnit)
	}
	unit := fmt.Sprintf(`[Unit]
Description=Lerd %s (%s)
%s
[Service]
Type=simple
Restart=%s
RestartSec=5
WorkingDirectory=%s
Environment=PATH=%s
SuccessExitStatus=1 130 143
ExecStart=/bin/sh -c '%s'

[Install]
WantedBy=default.target
`, label, siteName, fpmOrder, restart, sitePath, envPath, escaped)

	_ = services.Mgr.RemoveTimerUnit(unitName)
	return services.Mgr.WriteServiceUnitIfChanged(unitName, unit)
}

// workerLogHint returns the hint for viewing worker logs on Linux.
// host is accepted for cross-platform API parity but Linux journalctl
// works for both host and containerised workers so it's ignored.
func workerLogHint(unitName string, host bool) string {
	_ = host
	return "journalctl --user -u " + unitName + " -f"
}

// restoreWorker is called from restoreSiteInfrastructure during `lerd start`,
// before phase 1 brings up containers. We only write the unit file and enable
// it; the actual Start happens in phase 2 of runStart once lerd-redis and the
// other infra containers are up. Starting here would race against container
// readiness and cause errors like "lerd-redis: name does not resolve".
func restoreWorker(siteName, sitePath, phpVersion, workerName string, w config.FrameworkWorker) {
	// A project-supplied host worker only restores on boot if the user already
	// approved its command; otherwise skip silently so a cloned repo's host worker
	// can't run unattended on reboot.
	if w.Host && w.ProjectOrigin {
		if allowed, _ := config.HostCommandAllowed(siteName, w.Command); !allowed {
			return
		}
	}
	// Resolve the same way WorkerStartForSite does so a project opted into
	// auto-reload keeps its reload command across lerd start and reboots,
	// instead of silently coming back in standard mode.
	command := resolveWorkerCommand(sitePath, workerName, w)
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
		feedback.Warn("writing worker unit %s: %v", unitName, err)
		return
	}
	if changed {
		enableTarget := unitName
		if w.Schedule != "" {
			enableTarget = unitName + ".timer"
		}
		if err := services.Mgr.Enable(enableTarget); err != nil {
			feedback.Warn("enable %s: %v", enableTarget, err)
		}
	}
}
