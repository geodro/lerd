package cli

import (
	"fmt"
	"io"

	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// SupportedPHPVersions lists the PHP versions lerd can build FPM images for.
var SupportedPHPVersions = []string{"8.1", "8.2", "8.3", "8.4", "8.5"}

// NewFetchCmd returns the fetch command.
func NewFetchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch [version...]",
		Short: "Pre-build PHP FPM images so first use isn't slow",
		Long:  "Pulls pre-built PHP-FPM base images from ghcr.io and applies local layers (mkcert CA, custom extensions).\nPass --local to skip the pull and build entirely from source.\nSkips any version whose image already exists.",
		RunE:  runFetch,
	}
	cmd.Flags().Bool("local", false, "Build images locally instead of pulling pre-built base images")
	return cmd
}

func runFetch(cmd *cobra.Command, args []string) error {
	local, _ := cmd.Flags().GetBool("local")

	versions := args
	if len(versions) == 0 {
		versions = SupportedPHPVersions
	}

	jobs := make([]BuildJob, len(versions))
	for i, v := range versions {
		ver := v
		jobs[i] = BuildJob{
			Label: "PHP " + ver,
			Run:   func(w io.Writer) error { return podman.BuildFPMImageTo(ver, local, w) },
		}
	}

	if err := RunParallel(jobs); err != nil {
		fmt.Printf("[WARN] some images failed to build: %v\n", err)
	}
	fmt.Println("\nAll requested PHP images ready.")
	return nil
}
