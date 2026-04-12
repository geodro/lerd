package podman

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// PodmanBin returns the full path to the podman binary. On macOS it searches
// well-known Homebrew locations when PATH is restricted (e.g. launchd services).
func PodmanBin() string {
	if p, err := exec.LookPath("podman"); err == nil {
		return p
	}
	for _, candidate := range []string{
		"/opt/homebrew/bin/podman",
		"/usr/local/bin/podman",
	} {
		if _, err := exec.Command(candidate, "--version").Output(); err == nil {
			return candidate
		}
	}
	return "podman"
}

// Cmd returns an exec.Cmd for podman with the given arguments, using PodmanBin()
// so the binary is found even under launchd's restricted PATH.
func Cmd(args ...string) *exec.Cmd {
	return exec.Command(PodmanBin(), args...)
}

// Run executes podman with the given arguments and returns stdout.
func Run(args ...string) (string, error) {
	cmd := exec.Command(PodmanBin(), args...)
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
	cmd := exec.Command(PodmanBin(), "pull", image)
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

// ServiceVersion extracts the major version from a built-in service's image tag.
// For example: mysql:8.0 → "8.0", postgis/postgis:16-3.5-alpine → "16",
// redis:7-alpine → "7", meilisearch:v1.7 → "1.7".
// Returns "" if the version cannot be determined.
func ServiceVersion(quadletName string) string {
	image := ServiceImage(quadletName)
	if image == "" {
		return ""
	}
	// Extract tag after the last colon.
	idx := strings.LastIndex(image, ":")
	if idx < 0 {
		return ""
	}
	tag := image[idx+1:]
	// Strip leading "v" prefix.
	tag = strings.TrimPrefix(tag, "v")
	// Take only the version part (before any dash-separated suffix like "-alpine").
	if dash := strings.Index(tag, "-"); dash > 0 {
		// Keep if it looks like a version (e.g. "8.0"), drop suffix like "-alpine".
		candidate := tag[:dash]
		if len(candidate) > 0 && candidate[0] >= '0' && candidate[0] <= '9' {
			return candidate
		}
	}
	// Return as-is if it starts with a digit.
	if len(tag) > 0 && tag[0] >= '0' && tag[0] <= '9' {
		return tag
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
