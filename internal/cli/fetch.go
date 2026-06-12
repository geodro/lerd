package cli

import (
	"fmt"
	"io"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// SupportedPHPVersions re-exports the canonical list from internal/config, the
// single source of truth shared with the FrankenPHP image picker.
var SupportedPHPVersions = config.SupportedPHPVersions

// LegacyPHPVersions is the frozen legacy tier built from Alpine 3.16 with an old
// bundled Node. Kept here next to SupportedPHPVersions so the single definition
// of "legacy" is reused (e.g. pest:browser, which needs a modern Node) instead
// of being duplicated as literals elsewhere.
var LegacyPHPVersions = []string{"7.4", "8.0"}

// IsSupportedPHPVersion reports whether v is a version lerd can install.
func IsSupportedPHPVersion(v string) bool {
	return config.IsSupportedPHPVersion(v)
}

// IsLegacyPHPVersion reports whether v belongs to the frozen legacy tier.
func IsLegacyPHPVersion(v string) bool {
	for _, s := range LegacyPHPVersions {
		if s == v {
			return true
		}
	}
	return false
}

// InstallPHPVersion builds the FPM image for the given version, registers its
// quadlet and starts the service, streaming build output to w. It is the
// programmatic entry point behind the UI's "add PHP version" flow.
func InstallPHPVersion(version string, w io.Writer) error {
	if !IsSupportedPHPVersion(version) {
		return fmt.Errorf("unsupported PHP version %q", version)
	}
	// Always emit a line so a streamed install shows progress even when the
	// image is already built and the build step produces no output.
	fmt.Fprintf(w, "Installing PHP %s...\n", version)
	if err := ensureFPMQuadletTo(version, w); err != nil {
		return err
	}
	fmt.Fprintf(w, "PHP %s installed and started.\n", version)
	return nil
}

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
