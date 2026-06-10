package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/geodro/lerd/internal/config"
	nodeDet "github.com/geodro/lerd/internal/node"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// NewNodeManageCmd returns the node:manage command, which opts the host into
// lerd-managed Node.js (fnm shims) after the fact, for users who declined at
// `lerd install` time.
func NewNodeManageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "node:manage",
		Short: "Let lerd manage Node.js (install fnm shims and a default version)",
		Args:  cobra.NoArgs,
		RunE:  runNodeManage,
	}
}

func runNodeManage(_ *cobra.Command, _ []string) error {
	if lerdManagesNode() {
		fmt.Println("lerd is already managing Node.js.")
		return nil
	}
	fnmPath := filepath.Join(config.BinDir(), "fnm")
	if _, err := os.Stat(fnmPath); err != nil {
		return fmt.Errorf("fnm not found at %s — run 'lerd install' first", fnmPath)
	}
	fmt.Println("Installing fnm-managed node/npm/npx shims into", config.BinDir())
	if err := addShellShims(true); err != nil {
		return fmt.Errorf("writing shims: %w", err)
	}
	ensureDefaultNode()
	// Host workers (Vite etc.) were generated to run directly or via bun while
	// Node was unmanaged; rewrite them so they route through fnm again.
	regenerateHostWorkers()
	fmt.Println("lerd is now managing Node.js. Pin a version per project with `lerd isolate:node <v>`.")
	return nil
}

// NewNodeUnmanageCmd returns the node:unmanage command, which removes lerd's
// node shims and the fnm-installed Node binaries, leaving a clean system so the
// user can rely on bun or their own system Node.
func NewNodeUnmanageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "node:unmanage",
		Short: "Stop managing Node.js: remove lerd's node shims and fnm-installed versions",
		Args:  cobra.NoArgs,
		RunE:  runNodeUnmanage,
	}
}

var fnmVersionRe = regexp.MustCompile(`v\d+\.\d+\.\d+`)

func runNodeUnmanage(_ *cobra.Command, _ []string) error {
	fnmPath := filepath.Join(config.BinDir(), "fnm")

	// Uninstall every fnm-managed Node version so no stale binaries linger.
	// Uses fnm's own listing so we hit its data dir wherever it lives.
	if _, err := os.Stat(fnmPath); err == nil {
		if out, err := exec.Command(fnmPath, "list").CombinedOutput(); err == nil {
			seen := map[string]bool{}
			for _, v := range fnmVersionRe.FindAllString(string(out), -1) {
				if seen[v] {
					continue
				}
				seen[v] = true
				if uo, uerr := exec.Command(fnmPath, "uninstall", v).CombinedOutput(); uerr != nil {
					fmt.Printf("  [WARN] fnm uninstall %s: %s\n", v, string(uo))
				} else {
					fmt.Printf("  removed Node %s\n", v)
				}
			}
		}
	}

	// Remove the node/npm/npx shims (addShellShims(false) deletes them), so the
	// user's system node/npm/npx are no longer masked on PATH.
	if err := addShellShims(false); err != nil {
		return fmt.Errorf("removing shims: %w", err)
	}

	// Existing host worker units still reference `fnm exec --using=… -- npm …`,
	// which now has no Node to run; rewrite them so they use bun (when present)
	// or the user's system Node directly.
	fmt.Println("Regenerating host worker units...")
	regenerateHostWorkers()

	fmt.Println("lerd is no longer managing Node.js.")
	if nodeDet.BunPath() != "" {
		fmt.Println("bun is installed, so JS host workers (e.g. Vite) now run through bun.")
	} else {
		fmt.Println("JS host workers (e.g. Vite) now use your system Node. Install bun or a system Node if you have neither.")
	}
	fmt.Println("Re-enable lerd-managed Node with `lerd node:manage`.")
	return nil
}

// regenerateHostWorkers rewrites and restarts every registered site's active
// host worker units (Vite and other host:true workers) so they pick up the
// current JS-runtime decision after node:manage / node:unmanage flips what Node
// is available. Best-effort: failures are warned, not fatal.
func regenerateHostWorkers() {
	reg, err := config.LoadSites()
	if err != nil {
		return
	}
	for _, s := range reg.Sites {
		RegenerateHostWorkersForSite(s)
	}
}

// RegenerateHostWorkersForSite rewrites and restarts (only when changed) the
// host worker units of one site, so a JS-runtime change (e.g. flipping
// js_runtime to bun from the dashboard) takes effect on its Vite/dev worker
// without a manual restart. Best-effort. Paused/ignored sites are skipped so a
// runtime toggle does not resurrect a worker the user stopped.
func RegenerateHostWorkersForSite(s config.Site) {
	if s.Paused || s.Ignored {
		return
	}
	proj, _ := config.LoadProjectConfig(s.Path)
	if proj == nil {
		return
	}
	// Host-proxy sites run their dev command (`env PORT=N npm run ...`) as a
	// host worker too but have no framework, so handle them directly — they
	// are exactly the npm-on-host commands that should switch to bun.
	if s.IsHostProxy() {
		if proj.Proxy != nil {
			if w, ok := hostProxyWorker(proj.Proxy); ok {
				regenerateWorkerUnit(s.Name, s.Path, "", hostProxyWorkerName, w, hostProxyWorkerUnit(s.Name))
			}
		}
		return
	}
	fw, ok := config.GetFrameworkForDir(s.Framework, s.Path)
	if !ok || fw.Workers == nil {
		return
	}
	phpVersion := s.PHPVersion
	if phpVersion == "" {
		if cfg, _ := config.LoadGlobal(); cfg != nil {
			phpVersion = cfg.PHP.DefaultVersion
		}
	}
	// Iterate the framework's host workers directly, not proj.Workers:
	// some host workers (Vite is replaces_build/per_worktree) are enabled
	// via the build flow and never persisted to the saved workers list.
	for w, wDef := range fw.Workers {
		if !wDef.Host {
			continue
		}
		regenerateWorkerUnit(s.Name, s.Path, phpVersion, w, wDef, "lerd-"+w+"-"+s.Name)
	}
}

// regenerateWorkerUnit rewrites one enabled host worker unit and restarts it
// only when its content actually changed, so a re-sync doesn't disrupt workers
// already on the right runtime. persist=false keeps .lerd.yaml untouched (Vite
// is a build-replacer that's intentionally not persisted). Best-effort.
func regenerateWorkerUnit(siteName, sitePath, phpVersion, workerName string, wDef config.FrameworkWorker, unitName string) {
	if !services.Mgr.IsEnabled(unitName) {
		return
	}
	// Snapshot the unit before rewriting so we only restart it when its
	// ExecStart actually changed. On macOS the unit is a launchd plist
	// elsewhere, so before is empty and we always fall through to restart.
	unitPath := filepath.Join(config.SystemdUserDir(), unitName+".service")
	before, _ := os.ReadFile(unitPath)
	if err := WorkerStartForSite(siteName, sitePath, phpVersion, workerName, wDef, false); err != nil {
		fmt.Printf("  [WARN] regenerating %s: %v\n", unitName, err)
		return
	}
	after, _ := os.ReadFile(unitPath)
	if len(before) > 0 && string(before) == string(after) {
		return
	}
	// WorkerStartForSite only writes the unit file; systemd caches the old
	// definition until a reload, so reload before the restart picks up the new
	// ExecStart (StartUnit no-ops on an already-active unit).
	_ = podman.DaemonReload()
	// Clear any failed / start-rate-limit state first: a worker that
	// crash-looped under the previous runtime (e.g. bun on a project bun can't
	// run) would otherwise refuse to restart when switched back, so the toggle
	// would not heal it.
	podman.ResetFailedUnit(unitName)
	if err := podman.RestartUnit(unitName); err != nil {
		fmt.Printf("  [WARN] restarting %s: %v\n", unitName, err)
	} else {
		fmt.Printf("  regenerated %s\n", unitName)
	}
}
