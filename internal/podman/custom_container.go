package podman

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// CustomContainerName returns the Podman container name for a site's custom
// container, e.g. "lerd-custom-nestapp".
func CustomContainerName(siteName string) string {
	return "lerd-custom-" + siteName
}

// CustomImageName returns the local image tag for a site's custom container,
// e.g. "lerd-custom-nestapp:local".
func CustomImageName(siteName string) string {
	return CustomContainerName(siteName) + ":local"
}

// ResolveContainerfile returns the absolute path to the Containerfile for a
// project. If cfg specifies a Containerfile it is resolved relative to
// projectPath; otherwise the default "Containerfile.lerd" is used.
func ResolveContainerfile(projectPath string, cfg *config.ContainerConfig) string {
	name := "Containerfile.lerd"
	if cfg != nil && cfg.Containerfile != "" {
		name = cfg.Containerfile
	}
	return filepath.Join(projectPath, name)
}

// ResolveBuildContext returns the build context directory for a custom
// container build. Defaults to the project root.
func ResolveBuildContext(projectPath string, cfg *config.ContainerConfig) string {
	if cfg != nil && cfg.BuildContext != "" && cfg.BuildContext != "." {
		return filepath.Join(projectPath, cfg.BuildContext)
	}
	return projectPath
}

// HasContainerfile returns true when the project directory contains a
// Containerfile.lerd (the default custom container definition).
func HasContainerfile(projectPath string) bool {
	_, err := os.Stat(filepath.Join(projectPath, "Containerfile.lerd"))
	return err == nil
}

// CustomImageExists returns true when the local image for a site's custom
// container is present in the podman store.
func CustomImageExists(siteName string) bool {
	return ImageExists(CustomImageName(siteName))
}

// CustomImageUpToDate returns true when the image exists and the stored
// Containerfile hash matches the current file on disk. Returns false when
// the image is missing, the hash file is missing, or the Containerfile
// has changed since the last build.
func CustomImageUpToDate(siteName, projectPath string, cfg *config.ContainerConfig) bool {
	if !CustomImageExists(siteName) {
		return false
	}
	stored := readContainerfileHash(siteName)
	if stored == "" {
		return false
	}
	current := hashContainerfile(projectPath, cfg)
	return current != "" && current == stored
}

// StoreContainerfileHash persists the MD5 hash of the current Containerfile
// so subsequent link calls can skip the build when the file hasn't changed.
func StoreContainerfileHash(siteName, projectPath string, cfg *config.ContainerConfig) {
	hash := hashContainerfile(projectPath, cfg)
	if hash == "" {
		return
	}
	dir := filepath.Join(config.DataDir(), "container-hashes")
	os.MkdirAll(dir, 0755)                                         //nolint:errcheck
	os.WriteFile(filepath.Join(dir, siteName), []byte(hash), 0644) //nolint:errcheck
}

// RemoveContainerfileHash removes the stored hash for a site.
func RemoveContainerfileHash(siteName string) {
	path := filepath.Join(config.DataDir(), "container-hashes", siteName)
	os.Remove(path) //nolint:errcheck
}

func hashContainerfile(projectPath string, cfg *config.ContainerConfig) string {
	path := ResolveContainerfile(projectPath, cfg)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", md5.Sum(data))
}

func readContainerfileHash(siteName string) string {
	path := filepath.Join(config.DataDir(), "container-hashes", siteName)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// BuildCustomImage builds the OCI image for a site's custom container from
// the user's Containerfile. The image is tagged as lerd-custom-{siteName}:local.
func BuildCustomImage(siteName, projectPath string, cfg *config.ContainerConfig) error {
	containerfile := ResolveContainerfile(projectPath, cfg)
	if _, err := os.Stat(containerfile); err != nil {
		return fmt.Errorf("containerfile not found: %s", containerfile)
	}

	buildCtx := ResolveBuildContext(projectPath, cfg)
	imageName := CustomImageName(siteName)

	cmd := exec.Command(PodmanBin(), "build", "-t", imageName, "-f", containerfile, buildCtx)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building custom image %s: %w", imageName, err)
	}
	return nil
}

// BuildCustomImageTo builds the custom image, writing progress to w.
func BuildCustomImageTo(siteName, projectPath string, cfg *config.ContainerConfig, w interface{ Write([]byte) (int, error) }) error {
	containerfile := ResolveContainerfile(projectPath, cfg)
	if _, err := os.Stat(containerfile); err != nil {
		return fmt.Errorf("containerfile not found: %s", containerfile)
	}

	buildCtx := ResolveBuildContext(projectPath, cfg)
	imageName := CustomImageName(siteName)

	cmd := exec.Command(PodmanBin(), "build", "-t", imageName, "-f", containerfile, buildCtx)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building custom image %s: %w", imageName, err)
	}
	return nil
}

// RemoveCustomImage removes the local image for a site's custom container.
func RemoveCustomImage(siteName string) error {
	imageName := CustomImageName(siteName)
	_ = exec.Command(PodmanBin(), "rmi", "-f", imageName).Run()
	return nil
}

// ContainerBaseImage reads the Containerfile for a project and returns the
// base image from the first FROM instruction, e.g. "node:20-alpine".
// Returns "" if the file cannot be read or has no FROM line.
func ContainerBaseImage(projectPath string, cfg *config.ContainerConfig) string {
	path := ResolveContainerfile(projectPath, cfg)
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "FROM ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				// Strip the registry prefix for cleaner display.
				image := parts[1]
				image = strings.TrimPrefix(image, "docker.io/library/")
				image = strings.TrimPrefix(image, "docker.io/")
				return image
			}
		}
	}
	return ""
}

// RemoveCustomContainer removes the stopped container for a site's custom
// container (force-remove to handle edge cases).
func RemoveCustomContainer(siteName string) {
	name := CustomContainerName(siteName)
	_ = exec.Command(PodmanBin(), "rm", "-f", name).Run()
}
