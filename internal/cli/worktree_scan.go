package cli

import (
	"fmt"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewWorktreeScanCmd returns the worktree:scan command.
func NewWorktreeScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "worktree:scan",
		Short: "Scan all sites for git worktrees and regenerate their nginx vhosts",
		Long:  "Detects all active git worktrees across registered sites, ensures their dependencies are set up, and regenerates nginx vhost configurations. Useful when worktrees were created while the watcher was not running or when vhosts need to be refreshed.",
		RunE:  runWorktreeScan,
	}
}

// ScanWorktrees generates vhosts for all existing worktrees across all
// main-repo sites. Returns the number of worktree vhosts generated and any
// error encountered during the scan.
func ScanWorktrees() (int, error) {
	reg, err := config.LoadSites()
	if err != nil {
		return 0, fmt.Errorf("loading sites: %w", err)
	}
	generated := 0
	for _, s := range reg.Sites {
		if s.Ignored || s.Paused {
			continue
		}
		worktrees, err := gitpkg.DetectWorktrees(s.Path, s.PrimaryDomain())
		if err != nil || len(worktrees) == 0 {
			continue
		}
		for _, wt := range worktrees {
			gitpkg.EnsureWorktreeDeps(s.Path, wt.Path, wt.Domain, s.Secured)
			podman.EnsurePathMounted(wt.Path, s.PHPVersion)
			var vhostErr error
			if s.Secured {
				vhostErr = nginx.GenerateWorktreeSSLVhost(s, wt.Domain, wt.Path, s.PHPVersion)
			} else {
				vhostErr = nginx.GenerateWorktreeVhost(s, wt.Domain, wt.Path, s.PHPVersion)
			}
			if vhostErr != nil {
				fmt.Printf("[WARN] worktree vhost for %s: %v\n", wt.Domain, vhostErr)
				continue
			}
			fmt.Printf("Worktree vhost: %s -> %s\n", wt.Branch, wt.Domain)
			generated++
		}
	}
	if generated > 0 {
		if err := nginx.Reload(); err != nil {
			return generated, fmt.Errorf("nginx reload: %w", err)
		}
	}
	return generated, nil
}

func runWorktreeScan(_ *cobra.Command, _ []string) error {
	count, err := ScanWorktrees()
	if err != nil {
		return err
	}
	if count == 0 {
		fmt.Println("No worktree vhosts to generate.")
	} else {
		fmt.Printf("Generated %d worktree vhost(s) and reloaded nginx.\n", count)
	}
	return nil
}
