package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewPhpCmd returns the php command — runs PHP in the appropriate FPM container.
func NewPhpCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "php [args...]",
		Short:              "Run PHP in the project's container (e.g. lerd php artisan migrate)",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE:               runPhp,
	}
}

func runPhp(_ *cobra.Command, args []string) error {
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

	home := os.Getenv("HOME")
	composerHome := os.Getenv("COMPOSER_HOME")
	if composerHome == "" {
		// Respect XDG: prefer ~/.config/composer, fall back to ~/.composer
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(home, ".config")
		}
		composerHome = filepath.Join(xdgConfig, "composer")
	}
	composerBin := filepath.Join(composerHome, "vendor", "bin")

	if running, _ := podman.ContainerRunning(container); !running {
		return fmt.Errorf("PHP %s FPM service is not running — start it with: %s", version, serviceStartHint(container))
	}

	ensureServicesForCwd(cwd)

	// If any positional arg is an absolute path to a file that exists on the
	// host but outside $HOME (e.g. /tmp/ide-phpinfo.php written by PhpStorm),
	// the container won't be able to read it since only $HOME is volume-mounted.
	// Stream the file through stdin and replace the arg with /dev/stdin.
	var stdinReader io.Reader = os.Stdin
	useTTY := term.IsTerminal(int(os.Stdin.Fd()))
	for i, arg := range args {
		if filepath.IsAbs(arg) && !strings.HasPrefix(arg, home+"/") && arg != home {
			if data, err := os.ReadFile(arg); err == nil {
				args[i] = "/dev/stdin"
				stdinReader = bytes.NewReader(data)
				useTTY = false
				break
			}
		}
	}

	execFlags := []string{"exec", "-i"}
	if useTTY {
		execFlags = append(execFlags, "-t")
	}

	cmdArgs := append(execFlags, "-w", cwd,
		"--env", "HOME="+home,
		"--env", "COMPOSER_HOME="+composerHome,
		"--env", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:"+composerBin,
		container, "php",
	)
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("podman", cmdArgs...)
	cmd.Stdin = stdinReader
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
