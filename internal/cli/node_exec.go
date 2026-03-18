package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	nodeDet "github.com/geodro/lerd/internal/node"
	"github.com/spf13/cobra"
)

// NewNodeCmd returns the node command.
func NewNodeCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "node [args...]",
		Short:              "Run node using the project's version via fnm",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runWithFnm("node", args)
		},
	}
}

// NewNpmCmd returns the npm command.
func NewNpmCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "npm [args...]",
		Short:              "Run npm using the project's node version via fnm",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runWithFnm("npm", args)
		},
	}
}

// NewNpxCmd returns the npx command.
func NewNpxCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "npx [args...]",
		Short:              "Run npx using the project's node version via fnm",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runWithFnm("npx", args)
		},
	}
}

func runWithFnm(bin string, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	version, err := nodeDet.DetectVersion(cwd)
	if err != nil {
		cfg, _ := config.LoadGlobal()
		version = cfg.Node.DefaultVersion
	}

	fnm := filepath.Join(config.BinDir(), "fnm")
	if _, err := os.Stat(fnm); err != nil {
		return fmt.Errorf("fnm not found at %s — run 'lerd install' first", fnm)
	}

	// Ensure the version is installed (suppress output — fnm prints even when already installed)
	installCmd := exec.Command(fnm, "install", version)
	_ = installCmd.Run() // best-effort

	cmdArgs := []string{"exec", "--using=" + version, "--", bin}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(fnm, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
