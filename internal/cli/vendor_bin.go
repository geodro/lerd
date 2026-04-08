package cli

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

// NewTestCmd returns the `lerd test` command — shortcut for `lerd artisan test`.
func NewTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "test [args...]",
		Short:              "Run framework tests (shortcut for `lerd artisan test`)",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, args []string) error {
			return runConsole(nil, append([]string{"test"}, args...))
		},
	}
}

// NewVendorBinCmd returns the hidden `lerd vendor-bin <name> [args...]` command
// used by the top-level fallback in main.go to dispatch composer-installed
// binaries discovered in the project's vendor/bin directory.
func NewVendorBinCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "vendor-bin <name> [args...]",
		Short:              "Run a binary from the project's vendor/bin directory",
		Hidden:             true,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cobra.MinimumNArgs(1)(nil, args)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			name := args[0]
			rest := args[1:]
			return RunPHP(cwd, append([]string{"vendor/bin/" + name}, rest...))
		},
	}
}

// VendorBinExists reports whether the project rooted at cwd has an executable
// vendor/bin/<name> on disk. Used by the top-level command-not-found fallback
// to decide whether to dispatch unknown subcommands to a composer binary.
func VendorBinExists(cwd, name string) bool {
	if name == "" || cwd == "" {
		return false
	}
	// Reject path separators — composer bins are flat filenames.
	for _, r := range name {
		if r == '/' || r == os.PathSeparator {
			return false
		}
	}
	info, err := os.Stat(filepath.Join(cwd, "vendor", "bin", name))
	if err != nil || info.IsDir() {
		return false
	}
	return true
}

// ListVendorBins returns the names of executable files in <cwd>/vendor/bin,
// sorted alphabetically. Returns an empty slice if the directory doesn't exist.
func ListVendorBins(cwd string) []string {
	dir := filepath.Join(cwd, "vendor", "bin")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var bins []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		bins = append(bins, e.Name())
	}
	sort.Strings(bins)
	return bins
}
