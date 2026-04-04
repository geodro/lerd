package podman

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// PodmanBin returns the full path to the podman binary, searching well-known
// Homebrew locations when PATH is restricted (e.g. launchd services on macOS).
func PodmanBin() string {
	return podmanBin()
}

// podmanBin returns the full path to the podman binary, searching well-known
// Homebrew locations when PATH is restricted (e.g. launchd services on macOS).
func podmanBin() string {
	if p, err := exec.LookPath("podman"); err == nil {
		return p
	}
	for _, candidate := range []string{
		"/opt/homebrew/bin/podman", // Apple Silicon Homebrew
		"/usr/local/bin/podman",    // Intel Homebrew
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "podman"
}

// Run executes podman with the given arguments and returns stdout.
func Run(args ...string) (string, error) {
	cmd := exec.Command(podmanBin(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("podman %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RunSilent executes podman with the given arguments, discarding output.
func RunSilent(args ...string) error {
	_, err := Run(args...)
	return err
}

// ImageExists returns true if the named image is present in the local store.
func ImageExists(image string) bool {
	return RunSilent("image", "exists", image) == nil
}

// PullImageTo pulls the named image, writing progress output to w.
func PullImageTo(image string, w io.Writer) error {
	cmd := exec.Command(podmanBin(), "pull", image)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pulling %s: %w", image, err)
	}
	return nil
}

// ServiceImage returns the OCI image name embedded in a named quadlet template.
// Returns "" if the quadlet or Image line is not found.
func ServiceImage(quadletName string) string {
	content, err := GetQuadletTemplate(quadletName + ".container")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "Image=") {
			return strings.TrimPrefix(line, "Image=")
		}
	}
	return ""
}

// ContainerRunning returns true if the named container is running.
func ContainerRunning(name string) (bool, error) {
	out, err := Run("inspect", "--format={{.State.Running}}", name)
	if err != nil {
		// container doesn't exist
		return false, nil
	}
	return strings.TrimSpace(out) == "true", nil
}

// ContainerExists returns true if the named container exists (running or not).
func ContainerExists(name string) (bool, error) {
	_, err := Run("inspect", "--format={{.Name}}", name)
	if err != nil {
		return false, nil
	}
	return true, nil
}
