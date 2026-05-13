package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/spf13/cobra"
)

// NewIsolateCmd returns the isolate command.
func NewIsolateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "isolate <version>",
		Short: "Pin the PHP version for the current directory",
		Args:  cobra.ExactArgs(1),
		RunE:  runIsolate,
	}
}

func runIsolate(_ *cobra.Command, args []string) error {
	version := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(cwd, ".php-version"), []byte(version+"\n"), 0644); err != nil {
		return fmt.Errorf("writing .php-version: %w", err)
	}

	// Worktree path: persist to its .lerd.yaml (creating the file if missing
	// so the override travels with the branch) and regenerate just the
	// worktree's nginx vhost so the new FPM upstream takes effect.
	if site, branch, ok := FindParentSiteForWorktree(cwd); ok {
		if err := config.SetWorktreePHPVersion(cwd, version); err != nil {
			return fmt.Errorf("updating .lerd.yaml: %w", err)
		}
		if err := regenerateWorktreeVhost(site, branch, version); err != nil {
			fmt.Printf("[WARN] regenerating worktree vhost: %v\n", err)
		} else {
			nginx.ReloadOrWarn("")
		}
		fmt.Printf("PHP version pinned to %s for worktree %s of %s\n", version, branch, site.Name)
		return nil
	}

	// Parent-site path: keep the legacy behaviour — write .lerd.yaml when it
	// exists, then re-link so the registry and nginx pick up the change.
	_ = config.SetProjectPHPVersion(cwd, version)
	fmt.Printf("PHP version pinned to %s\n", version)

	if _, err := config.FindSiteByPath(cwd); err == nil {
		if err := runLink([]string{}); err != nil {
			fmt.Printf("[WARN] re-linking site: %v\n", err)
		}
	}

	return nil
}

// regenerateWorktreeVhost rewrites a single worktree's nginx vhost using the
// supplied PHP version, picking the secured/unsecured template based on the
// parent site's TLS state.
func regenerateWorktreeVhost(site *config.Site, branch, phpVersion string) error {
	worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil {
		return err
	}
	for _, wt := range worktrees {
		if wt.Branch != branch {
			continue
		}
		if site.Secured {
			return nginx.GenerateWorktreeSSLVhost(wt.Domain, wt.Path, phpVersion, site.PrimaryDomain(), site.Name, wt.Branch)
		}
		return nginx.GenerateWorktreeVhost(wt.Domain, wt.Path, phpVersion, site.Name, wt.Branch)
	}
	return fmt.Errorf("worktree %q not found", branch)
}
