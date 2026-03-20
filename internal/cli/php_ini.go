package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewPhpIniCmd returns the php:ini command.
func NewPhpIniCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "php:ini [version]",
		Short: "Edit the user php.ini for a PHP version",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPhpIni,
	}
}

func runPhpIni(_ *cobra.Command, args []string) error {
	version, err := phpExtVersion(args)
	if err != nil {
		return err
	}

	if err := podman.EnsureUserIni(version); err != nil {
		return fmt.Errorf("creating user ini: %w", err)
	}

	path := config.PHPUserIniFile(version)
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		// Fall back to common editors in order of preference
		for _, e := range []string{"nano", "vim", "vi"} {
			if _, err := exec.LookPath(e); err == nil {
				editor = e
				break
			}
		}
	}
	if editor == "" {
		fmt.Printf("User ini file: %s\n", path)
		fmt.Println("Set $EDITOR to open it automatically.")
		return nil
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor exited: %w", err)
	}

	// Ensure the quadlet has the user ini volume mount (may be missing on
	// installations predating the user ini feature).
	if err := podman.WriteFPMQuadlet(version); err != nil {
		return fmt.Errorf("updating quadlet: %w", err)
	}

	short := strings.ReplaceAll(version, ".", "")
	unit := "lerd-php" + short + "-fpm"
	fmt.Printf("Saved. Restarting %s...\n", unit)
	if err := podman.RestartUnit(unit); err != nil {
		return fmt.Errorf("restarting %s: %w", unit, err)
	}
	fmt.Println("Done.")
	return nil
}
