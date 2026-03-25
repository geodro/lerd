package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/spf13/cobra"
)

// NewLogsCmd returns the logs command.
func NewLogsCmd() *cobra.Command {
	var follow bool
	var lines int

	cmd := &cobra.Command{
		Use:   "logs [target]",
		Short: "Show logs for the current project's PHP-FPM container, nginx, or a service",
		Long: `Show container logs.

Target can be:
  (none)       — PHP-FPM container for the current project
  nginx        — nginx container
  <service>    — any known service (mysql, redis, mailpit, etc.)
  <version>    — explicit PHP version, e.g. "8.4"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			container, err := resolveLogsTarget(args)
			if err != nil {
				return err
			}

			cmdArgs := []string{"logs"}
			if follow {
				cmdArgs = append(cmdArgs, "-f")
			}
			if lines > 0 {
				cmdArgs = append(cmdArgs, "--tail", fmt.Sprintf("%d", lines))
			}
			cmdArgs = append(cmdArgs, container)

			cmd := exec.Command("podman", cmdArgs...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "Number of lines to show from the end (0 = all)")

	return cmd
}

func resolveLogsTarget(args []string) (string, error) {
	if len(args) == 0 {
		return phpFPMContainer()
	}

	target := args[0]

	// nginx
	if target == "nginx" {
		return "lerd-nginx", nil
	}

	// known service name
	for _, svc := range knownServices {
		if target == svc {
			return "lerd-" + svc, nil
		}
	}

	// explicit PHP version like "8.4" or "8.5"
	if strings.Contains(target, ".") {
		short := strings.ReplaceAll(target, ".", "")
		return "lerd-php" + short + "-fpm", nil
	}

	// registered site name — resolve to its PHP-FPM container
	if site, err := config.FindSite(target); err == nil {
		short := strings.ReplaceAll(site.PHPVersion, ".", "")
		return "lerd-php" + short + "-fpm", nil
	}

	return "", fmt.Errorf("unknown log target %q — use nginx, a service name, a PHP version (e.g. 8.5), a site name, or omit for the current project's FPM container", target)
}

func phpFPMContainer() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	version, err := phpDet.DetectVersion(cwd)
	if err != nil {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			return "", fmt.Errorf("cannot detect PHP version: %w", err)
		}
		version = cfg.PHP.DefaultVersion
	}

	short := strings.ReplaceAll(version, ".", "")
	return "lerd-php" + short + "-fpm", nil
}
