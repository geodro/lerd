package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
)

// NewNewCmd returns the new command — scaffold a new PHP project.
func NewNewCmd() *cobra.Command {
	var frameworkName string

	cmd := &cobra.Command{
		Use:   "new <name-or-path>",
		Short: "Scaffold a new PHP project",
		Long: `Create a new PHP project using the framework's scaffold command.

  lerd new myapp                          # create ./myapp using Laravel (default)
  lerd new myapp --framework=symfony      # create ./myapp using Symfony
  lerd new /path/to/myapp                 # create at an absolute path
  lerd new myapp -- --no-interaction      # pass extra args to the scaffold command

For Laravel this runs:
  composer create-project laravel/laravel <target> [extra args]

Other frameworks must define a 'create' field in their YAML definition:
  create: composer create-project myvendor/myframework

After creation, register the site with:
  cd <target>
  lerd link
  lerd setup`,
		Args:                  cobra.MinimumNArgs(1),
		DisableFlagsInUseLine: true,
		SilenceUsage:          true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			extraArgs := args[1:]
			return runNew(target, frameworkName, extraArgs)
		},
	}

	// Stop flag parsing after the first positional arg so extra flags
	// (e.g. --no-interaction) are passed through to the scaffold command.
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringVar(&frameworkName, "framework", "laravel", "Framework to use")

	return cmd
}

func runNew(target, frameworkName string, extraArgs []string) error {
	// Resolve target to an absolute path
	if !filepath.IsAbs(target) {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		target = filepath.Join(cwd, target)
	}

	// Look up the framework
	fw, ok := config.GetFramework(frameworkName)
	if !ok {
		return fmt.Errorf("unknown framework %q — run 'lerd framework list' to see available frameworks", frameworkName)
	}
	if fw.Create == "" {
		return fmt.Errorf("framework %q has no create command — add a 'create' field to its YAML definition", frameworkName)
	}

	// Build the full command: <create command parts> <target> [extra args]
	parts := strings.Fields(fw.Create)
	parts = append(parts, target)
	parts = append(parts, extraArgs...)

	fmt.Printf("Creating new %s project at %s\n", fw.Label, target)
	fmt.Printf("Running: %s\n\n", strings.Join(parts, " "))

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scaffold command failed: %w", err)
	}

	fmt.Printf("\nProject created at %s\n", target)
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  cd %s\n", target)
	fmt.Printf("  lerd link\n")
	fmt.Printf("  lerd setup\n")
	return nil
}
