package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewPhpShellCmd returns the shell command — opens an interactive sh session in the PHP-FPM container.
func NewPhpShellCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "shell",
		Short:        "Open a shell in the project's PHP-FPM container",
		SilenceUsage: true,
		RunE:         runPhpShell,
	}
}

func runPhpShell(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	version, err := phpDet.DetectVersion(cwd)
	if err != nil {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			return fmt.Errorf("cannot detect PHP version: %w", err)
		}
		version = cfg.PHP.DefaultVersion
	}

	short := strings.ReplaceAll(version, ".", "")
	container := "lerd-php" + short + "-fpm"

	if running, _ := podman.ContainerRunning(container); !running {
		return fmt.Errorf("PHP %s FPM container is not running — start it with: systemctl --user start %s", version, container)
	}

	// Use the registered site root as the working directory if cwd is inside one,
	// otherwise fall back to cwd.
	workDir := siteRootFor(cwd)

	podman.EnsurePathMounted(workDir, version)
	ensureServicesForCwd(workDir)

	cmd := podman.Cmd("exec", "-it", "-w", workDir, container, "sh")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		return err
	}
	return nil
}

// siteRootFor returns the registered site path that contains dir, or dir itself
// if no registered site matches.
func siteRootFor(dir string) string {
	reg, err := config.LoadSites()
	if err != nil {
		return dir
	}
	// Normalise to clean absolute path for prefix matching.
	dir = filepath.Clean(dir)
	best := ""
	for _, s := range reg.Sites {
		sitePath := filepath.Clean(s.Path)
		// dir == sitePath or dir is underneath sitePath
		if dir == sitePath || strings.HasPrefix(dir, sitePath+string(filepath.Separator)) {
			// Prefer the longest (most-specific) match.
			if len(sitePath) > len(best) {
				best = sitePath
			}
		}
	}
	if best != "" {
		return best
	}
	return dir
}
