package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
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

	if err := pinProjectPHP(cwd, version); err != nil {
		return err
	}

	fmt.Printf("PHP version pinned to %s\n", version)

	// Re-link if the site is registered so nginx picks up the new version.
	if _, err := config.FindSiteByPath(cwd); err == nil {
		if err := runLink([]string{}); err != nil {
			fmt.Printf("[WARN] re-linking site: %v\n", err)
		}
	}

	return nil
}

// pinProjectPHP writes the PHP version pin files for a project directory.
// It always writes .php-version (so CLI php and other tools see it).
// It also updates php_version in .lerd.yaml when that file already exists,
// so the lerd override (priority 1) stays in sync.
func pinProjectPHP(dir, version string) error {
	if err := os.WriteFile(filepath.Join(dir, ".php-version"), []byte(version+"\n"), 0644); err != nil {
		return fmt.Errorf("writing .php-version: %w", err)
	}

	_ = config.SetProjectPHPVersion(dir, version)

	return nil
}
