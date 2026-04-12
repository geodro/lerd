package cli

import (
	"fmt"

	"github.com/geodro/lerd/internal/version"
	"github.com/spf13/cobra"
)

// NewAboutCmd returns the about command.
func NewAboutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "about",
		Short: "Show information about Lerd",
		RunE:  runAbout,
	}
}

func runAbout(_ *cobra.Command, _ []string) error {
	fmt.Println("Lerd — Podman-powered local PHP development environment for Linux and macOS")
	fmt.Println()
	fmt.Printf("  Version  %s\n", version.Version)
	fmt.Printf("  Commit   %s\n", version.Commit)
	fmt.Printf("  Built    %s\n", version.Date)
	fmt.Println()
	fmt.Println("  https://github.com/geodro/lerd")
	fmt.Println()
	fmt.Println("  (c) George Dumitrescu")
	return nil
}
