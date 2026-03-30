package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewConsoleCmd returns the console command — runs framework console in the project's container.
func NewConsoleCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "console [args...]",
		Aliases:            []string{"artisan"},
		Short:              "Run framework console command in the project's container",
		Example:            "  lerd console cache:clear\n  lerd console make:controller User",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE:               runConsole,
	}
}

func runConsole(_ *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Get console command for current framework
	consoleCmd, err := getConsoleCommand(cwd)
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

	ensureServicesForCwd(cwd)

	execFlags := []string{"exec", "-i"}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		execFlags = append(execFlags, "-t")
	}
	cmdArgs := append(execFlags, "-w", cwd, container, "php", consoleCmd)
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("podman", cmdArgs...)
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

func getConsoleCommand(cwd string) (string, error) {
	framework := ""

	// Try to get framework from site config first
	siteConfig, err := config.FindSiteByPath(cwd)
	if err == nil && siteConfig.Framework != "" {
		framework = siteConfig.Framework
	} else {
		// Detect framework from directory
		detectedFramework, ok := config.DetectFramework(cwd)
		if !ok {
			return "", fmt.Errorf("no framework detected — create framework config with 'lerd framework add'")
		}

		framework = detectedFramework
	}

	// Get framework definition
	frameworkConfig, ok := config.GetFramework(framework)
	if !ok {
		return "", fmt.Errorf("framework %q not found", framework)
	}

	if frameworkConfig.Console == "" {
		return "", fmt.Errorf(
			"no console command defined for framework %q — add 'console' field to %s/%s.yaml",
			frameworkConfig.Name,
			config.FrameworksDir(),
			frameworkConfig.Name,
		)
	}

	return frameworkConfig.Console, nil
}
