package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/feedback"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteops"
	"github.com/spf13/cobra"
)

// Wire worker teardown into the shared unlink core so every unlink path (CLI,
// MCP, parked-directory watcher) stops the site's workers — including a
// host-proxy site's always-restart dev server.
func init() {
	siteops.StopSiteWorkers = func(site *config.Site) {
		for _, w := range collectRunningWorkers(site) {
			stopWorkerByName(site, w)
		}
		// collectRunningWorkers only reports active units (correct for pause,
		// which resumes them later). On unlink the site is going away, so tear
		// the dev-server unit down unconditionally — a stopped or failed one
		// would otherwise orphan its .service file. stopWorkerUnit is idempotent.
		if site.IsHostProxy() {
			WorkerStopForSite(site.Name, site.Path, hostProxyWorkerName) //nolint:errcheck
		}
		// Worktrees run their own per-worktree units (lerd-<worker>-<site>-<slug>)
		// that collectRunningWorkers (parent-only) misses. On unlink the site is
		// going away, so stop every worktree's workers too — a host-proxy
		// Restart=always dev server would otherwise loop against a removed dir.
		if wts, err := gitpkg.ServableWorktrees(site.Path, site.PrimaryDomain()); err == nil {
			for _, wt := range wts {
				StopAllWorkersForWorktree(site.Name, filepath.Base(wt.Path)) //nolint:errcheck
			}
		}
		// Path-independent backstop: when the site dir is already gone (watcher
		// prune), worktree detection above finds nothing, so sweep any remaining
		// worker units for this site by name. Idempotent with the loops above.
		stopAllSiteWorkerUnits(site)
	}
}

// NewUnlinkCmd returns the unlink command.
func NewUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink",
		Short: "Unlink the current directory site",
		Args:  cobra.NoArgs,
		RunE:  runUnlink,
	}
}

func runUnlink(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return fmt.Errorf("no site registered for %s — link it first with lerd link", cwd)
	}
	feedback.Begin()
	unlinkErr := UnlinkSite(site.Name)
	feedback.Begin()
	return unlinkErr
}

// UnlinkSite removes the nginx vhost for the named site. For sites under a parked
// directory, the registry entry is kept but marked Ignored so the watcher does not
// re-register it. For manually-linked sites the entry is removed entirely.
func UnlinkSite(name string) error {
	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found", name)
	}

	// Workers are stopped by UnlinkSiteCore via the siteops.StopSiteWorkers
	// hook registered in this package's init, so every unlink path tears them
	// down uniformly.

	cfg, _ := config.LoadGlobal()
	var parkedDirs []string
	if cfg != nil {
		parkedDirs = cfg.ParkedDirectories
	}

	step := feedback.Start("unlinking " + name)
	if err := siteops.UnlinkSiteCore(site, parkedDirs); err != nil {
		step.Fail(err)
		return err
	}
	step.OK(feedback.Val(site.PrimaryDomain()))

	// Offer to remove the cached custom container image.
	if site.IsCustomContainer() && podman.CustomImageExists(site.Name) {
		if isInteractive() && feedback.Confirm("Remove the container image?", false) {
			_ = podman.RemoveCustomImage(site.Name)
			podman.RemoveContainerfileHash(site.Name)
			feedback.Line("image removed")
		}
	}

	// Offer to remove the framework definition when this was the last site
	// using it and the definition is removable (store-installed or user-defined,
	// never a built-in). It is safe to remove: lerd re-fetches it from the store
	// the moment another site needs it.
	offerRemoveOrphanedFramework(site.Framework)

	autoStopUnusedServices()
	autoStopUnusedFPMs()

	return nil
}

// offerRemoveOrphanedFramework prompts, in interactive sessions only, to delete
// a framework definition once no remaining active site references it. Built-in
// frameworks are never offered.
func offerRemoveOrphanedFramework(fw string) {
	if !isInteractive() || !frameworkIsOrphaned(fw) {
		return
	}
	if feedback.Confirm(fmt.Sprintf("No sites use the %q framework anymore. Remove its definition?", fw), false) {
		if err := config.RemoveFramework(fw); err != nil {
			feedback.Warn("could not remove framework %q: %v", fw, err)
			return
		}
		feedback.Line("framework definition removed")
	}
}

// frameworkIsOrphaned reports whether fw has a removable definition (store or
// user, never built-in) that no remaining active site references.
func frameworkIsOrphaned(fw string) bool {
	if fw == "" {
		return false
	}
	for _, name := range config.UnusedInstalledFrameworks() {
		if name == fw {
			return true
		}
	}
	return false
}
