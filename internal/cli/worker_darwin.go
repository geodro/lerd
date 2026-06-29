//go:build darwin

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/feedback"
	nodeDet "github.com/geodro/lerd/internal/node"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
)

// defaultMacOSNodeVersion is the version `fnm exec --using=…` falls
// back to when the site has no detectable .nvmrc / package.json engines
// and config.Node.DefaultVersion is unset. Picked to match the Linux
// path's defaultNodeVersion so host workers behave the same across
// platforms by default.
const defaultMacOSNodeVersion = "22"

// writeWorkerUnitFile writes the macOS launch artifacts for a framework
// worker. Three shapes:
//
//   - host: true → a launchd plist whose ExecStart runs a guard script
//     that `cd`s into the site/worktree and `fnm exec --using=<node>`s
//     the command on the host. No podman involvement. Used for Vite +
//     other Node tooling that needs direct host access (HMR, file
//     watchers, etc.).
//   - cfg.WorkerExecMode() == "exec" (default for containerised
//     workers): a service unit whose ExecStart runs a guard script that
//     `podman exec`s into the shared FPM container.
//   - cfg.WorkerExecMode() == "container": one detached container per
//     worker, spawned from the FPM image.
//
// Scheduled workers (Schedule != "") still aren't supported on macOS —
// launchd's StartCalendarInterval would work but the unit translation
// isn't wired through services.Mgr yet.
func writeWorkerUnitFile(unitName, label, siteName, sitePath, phpVersion, command, restart, schedule, fpmUnit string, host bool) (bool, error) {
	// Generation-boundary guard so every caller is covered (incl. boot
	// restore): a newline would break out of the launchd guard-script line
	// from a cloned repo's .lerd.yaml custom_workers entry.
	if config.ContainsUnitInjectionChars(command) {
		return false, fmt.Errorf("worker unit %q: command must not contain newline or NUL", unitName)
	}
	if schedule != "" {
		feedback.Warn("worker %s has schedule=%q which is not yet supported on macOS — skipping", unitName, schedule)
		return false, nil
	}
	if host {
		return writeWorkerHostUnit(unitName, sitePath, command, restart)
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

// writeWorkerHostUnit is the `host: true` path: write a guard script
// that resolves the node version via fnm and exec's the worker command
// in the site/worktree directory. The launchd plist supervises the
// outer /bin/sh; KeepAlive=true (from Restart=always|on-failure)
// restarts it on exit.
//
// Lives under run/workers alongside the exec-mode guard scripts so
// removeWorkerExecArtifacts cleans both up on stop.
func writeWorkerHostUnit(unitName, sitePath, command, restart string) (bool, error) {
	workersDir := filepath.Join(config.RunDir(), "workers")
	if err := os.MkdirAll(workersDir, 0755); err != nil {
		return false, fmt.Errorf("creating worker run dir: %w", err)
	}
	scriptPath := filepath.Join(workersDir, unitName+".sh")

	fnmBin := filepath.Join(config.BinDir(), "fnm")
	// bun projects rewrite npm/npx/node to bun and run it directly (no fnm),
	// with ~/.bun/bin added to PATH; Node projects resolve a version via fnm;
	// host-proxy sites in any other language run the command directly.
	nodeVersion := ""
	bunDir := ""
	if bun := bunRunnerFor(sitePath, false); bun != "" {
		command = nodeDet.Bunify(command)
		bunDir = filepath.Dir(bun)
	} else if isNodeProject(sitePath) && lerdManagesNode() {
		// Only pin via fnm when lerd manages Node; otherwise nodeVersion stays
		// empty and the guard script runs the command directly against the
		// user's system node (after node:unmanage there is no fnm Node).
		nodeVersion = resolveNodeVersionForHostWorker(sitePath)
	}

	script := buildDarwinHostWorkerGuardScript(fnmBin, config.BinDir(), nodeVersion, sitePath, command, bunDir)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return false, fmt.Errorf("writing host worker guard script: %w", err)
	}

	unit := buildDarwinHostWorkerService(scriptPath, restart)
	if err := services.Mgr.WriteServiceUnit(unitName, unit); err != nil {
		return false, err
	}
	if err := podman.DaemonReloadFn(); err != nil {
		return false, err
	}
	return true, nil
}

// resolveNodeVersionForHostWorker picks the node version a host worker
// should run with at sitePath. Order: detector (reads .nvmrc /
// package.json engines), then cfg.Node.DefaultVersion, then the
// hard-coded default. fnm accepts both major-only ("22") and pinned
// ("22.15.0") strings, so we don't normalise.
func resolveNodeVersionForHostWorker(sitePath string) string {
	if v, err := nodeDet.DetectVersion(sitePath); err == nil && v != "" {
		return v
	}
	if cfg, _ := config.LoadGlobal(); cfg != nil && cfg.Node.DefaultVersion != "" {
		return cfg.Node.DefaultVersion
	}
	return defaultMacOSNodeVersion
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

	// Resolve the container to exec into via the shared helper, which
	// handles custom container + FrankenPHP + default shared FPM. fpmUnit
	// is the same value the Linux backend sets via BindsTo=, kept on the
	// signature for API parity.
	_ = fpmUnit
	container := resolveWorkerFPMUnit(siteName, phpVersion)

	podmanExec := fmt.Sprintf("%s exec -w %s %s %s", podman.PodmanBin(), sitePath, container, command)
	script := buildDarwinExecWorkerGuardScript(pidFile, podman.PodmanBin(), container, sitePath, command, podmanExec)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return false, fmt.Errorf("writing worker guard script: %w", err)
	}

	// Persist the reap command so stop can terminate the in-container worker.
	// Stopping the launchd job only kills the host-side `podman exec`; without
	// this the in-container process (and its file-watcher child) is orphaned,
	// which is why idle-suspended sites kept burning CPU. Best-effort: a failure
	// here only degrades stop cleanup, it must not block starting the worker.
	reapPath := filepath.Join(workersDir, unitName+".reap")
	reapCmd := buildWorkerReapCommand(podman.PodmanBin(), container, sitePath, command)
	if err := os.WriteFile(reapPath, []byte(reapCmd+"\n"), 0644); err != nil {
		feedback.Warn("worker %s: writing reap sidecar: %v (stop may leave an orphan)", unitName, err)
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
// Host-mode and exec-mode workers always log to ~/Library/Logs/lerd —
// launchd writes the unit's stdout/stderr there via the plist's
// StandardOutPath / StandardErrorPath. Container-mode (FPM-bound)
// workers log to their own podman container. host=true overrides the
// container-mode check because host workers never have a container.
func workerLogHint(unitName string, host bool) string {
	if !host {
		if cfg, _ := config.LoadGlobal(); cfg != nil && cfg.WorkerExecMode() == config.WorkerExecModeContainer {
			return "podman logs -f " + unitName
		}
	}
	home, _ := os.UserHomeDir()
	return "tail -f " + filepath.Join(home, "Library", "Logs", "lerd", unitName+".log")
}

// removeWorkerExecArtifacts deletes the on-disk files writeWorkerExecUnit
// produces alongside the launchd plist: the guard shell script and its
// pid file. Both live in config.RunDir()/workers and are macOS-only —
// the Linux build provides a stub.
//
// Called on every worker stop so the artifacts don't outlive the unit
// (an orphan script with no plist isn't actively harmful but accumulates
// noise in ~/.local/share/lerd/run/workers and can confuse later
// migration / discovery code).
func removeWorkerExecArtifacts(unitName string) {
	workersDir := filepath.Join(config.RunDir(), "workers")
	// Reap the in-container worker before dropping its artifacts: the launchd
	// stop already killed the host-side `podman exec`, but the process it
	// started inside the FPM container (and its file-watcher child) survives.
	reapInContainerWorker(filepath.Join(workersDir, unitName+".reap"))
	_ = os.Remove(filepath.Join(workersDir, unitName+".sh"))
	_ = os.Remove(filepath.Join(workersDir, unitName+".pid"))
	_ = os.Remove(filepath.Join(workersDir, unitName+".reap"))
}

// reapInContainerWorker runs the worker's persisted .reap command so the
// in-container process — and its children, e.g. the octane/horizon
// file-watcher — are terminated when the launchd job stops. Best-effort and
// time-boxed so a hung podman never blocks a stop; absent on workers started by
// an older build (no sidecar) or the host/container modes.
func reapInContainerWorker(reapPath string) {
	cmd, err := os.ReadFile(reapPath)
	if err != nil || strings.TrimSpace(string(cmd)) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "sh", "-c", string(cmd)).Run()
}

// restoreWorker is called from restoreSiteInfrastructure during `lerd start`.
// On macOS we only write the unit file; the actual start is deferred to
// phase 2 of runStart so we don't saturate the Podman Machine SSH connection
// before containers are ready.
func restoreWorker(siteName, sitePath, phpVersion, workerName string, w config.FrameworkWorker) {
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
	// Resolve the same way WorkerStartForSite does so a project opted into
	// auto-reload keeps its reload command across lerd start and reboots.
	command := resolveWorkerCommand(sitePath, workerName, w)
	writeWorkerUnitFile(unitName, label, displaySite, sitePath, phpVersion, command, restart, w.Schedule, fpmUnit, w.Host) //nolint:errcheck
}
