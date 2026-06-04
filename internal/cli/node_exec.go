package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
		SilenceUsage:       true,
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
		SilenceUsage:       true,
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
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runWithFnm("npx", args)
		},
	}
}

// runNpmCaptured runs `npm <args>` in dir using the project's Node version via
// fnm, capturing combined output. Unlike runWithFnm (which streams to the
// terminal and os.Exit's on failure for CLI use), this is for non-interactive
// callers like the UI: it returns the output and never exits the process, and
// it surfaces a failed `fnm install` instead of swallowing it. Shares the same
// fnm lookup, version detection, and npm_config_prefix handling as runWithFnm.
func runNpmCaptured(dir string, args ...string) (string, error) {
	fnm := filepath.Join(config.BinDir(), "fnm")
	if _, err := os.Stat(fnm); err != nil {
		return "", fmt.Errorf("fnm not found at %s, run 'lerd install' first", fnm)
	}

	version, _ := nodeDet.DetectVersion(dir)
	if version == "" {
		version = "default"
	}
	if version != "default" {
		if out, err := exec.Command(fnm, "install", version).CombinedOutput(); err != nil {
			return "", fmt.Errorf("installing Node %s via fnm: %s", version, strings.TrimSpace(string(out)))
		}
	} else if exec.Command(fnm, "exec", "--using=default", "--", "true").Run() != nil {
		return "", fmt.Errorf("no Node.js version available via lerd, run: lerd node:install 22")
	}

	cmdArgs := append([]string{"exec", "--using=" + version, "--", "npm"}, args...)
	cmd := exec.Command(fnm, cmdArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "npm_config_prefix="+config.NodeGlobalDir())
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runWithFnm(bin string, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	version, _ := nodeDet.DetectVersion(cwd)
	// Empty means the user has no .nvmrc / .node-version / global default; fall
	// through to the fnm `default` alias so we still surface a friendly error
	// instead of an unhelpful "Can't find version in dotfiles".
	if version == "" {
		version = "default"
	}

	fnm := filepath.Join(config.BinDir(), "fnm")
	if _, err := os.Stat(fnm); err != nil {
		return fmt.Errorf("fnm not found at %s — run 'lerd install' first", fnm)
	}

	if version != "default" {
		_ = exec.Command(fnm, "install", version).Run()
	} else if exec.Command(fnm, "exec", "--using=default", "--", "true").Run() != nil {
		return fmt.Errorf("no Node.js version available via lerd — run: lerd node:install 22")
	}

	cmdArgs := []string{"exec", "--using=" + version, "--", bin}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(fnm, cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	manageGlobals := bin == "npm" || bin == "npx"
	prefix := config.NodeGlobalDir()
	if manageGlobals {
		if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err == nil {
			cmd.Env = append(os.Environ(), "npm_config_prefix="+prefix)
		}
	}
	runErr := cmd.Run()
	if manageGlobals {
		if syncErr := syncNodeGlobalBins(filepath.Join(prefix, "bin"), config.BinDir(), fnm); syncErr != nil {
			fmt.Fprintf(os.Stderr, "lerd: warning: failed to sync npm global wrappers: %v\n", syncErr)
		}
	}
	if runErr != nil {
		if exit, ok := runErr.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		return runErr
	}
	return nil
}
