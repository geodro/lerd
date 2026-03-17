package podman

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// BuildFPMImage builds the lerd PHP-FPM image for the given version if it doesn't exist.
// Prints build output to stdout so the user can see progress.
func BuildFPMImage(version string) error {
	return buildFPMImage(version, false)
}

// RebuildFPMImage force-removes and rebuilds the PHP-FPM image for the given version.
func RebuildFPMImage(version string) error {
	return buildFPMImage(version, true)
}

func buildFPMImage(version string, force bool) error {
	short := strings.ReplaceAll(version, ".", "")
	imageName := "lerd-php" + short + "-fpm:local"

	if !force {
		// Skip if image already exists
		checkCmd := exec.Command("podman", "image", "exists", imageName)
		if checkCmd.Run() == nil {
			return nil
		}
	} else {
		// Remove existing image so we get a clean rebuild
		rmCmd := exec.Command("podman", "rmi", "-f", imageName)
		_ = rmCmd.Run() // ignore error if image didn't exist
	}

	fmt.Printf("\n  Building PHP %s image (may take a few minutes)...\n", version)

	containerfileTmpl, err := GetQuadletTemplate("lerd-php-fpm.Containerfile")
	if err != nil {
		return err
	}
	containerfile := strings.ReplaceAll(containerfileTmpl, "{{.Version}}", version)

	tmp, err := os.MkdirTemp("", "lerd-php-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	cfPath := tmp + "/Containerfile"
	if err := os.WriteFile(cfPath, []byte(containerfile), 0644); err != nil {
		return err
	}

	cmd := exec.Command("podman", "build", "-t", imageName, "-f", cfPath, tmp)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building PHP %s image: %w", version, err)
	}

	fmt.Printf("  PHP %s image built successfully.\n", version)
	return nil
}
