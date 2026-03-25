//go:build darwin

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
)

func downloadBinaries(w io.Writer) error {
	arch := runtime.GOARCH
	binDir := config.BinDir()

	// composer
	composerPharPath := filepath.Join(binDir, "composer.phar")
	if _, err := os.Stat(composerPharPath); os.IsNotExist(err) {
		if err := downloadFile("https://getcomposer.org/composer-stable.phar", composerPharPath, 0755, w); err != nil {
			return fmt.Errorf("composer download: %w", err)
		}
	}

	// fnm — macOS universal binary
	fnmPath := filepath.Join(binDir, "fnm")
	if _, err := os.Stat(fnmPath); os.IsNotExist(err) {
		fnmZip := filepath.Join(binDir, "fnm-macos.zip")
		if err := downloadFile(
			"https://github.com/Schniz/fnm/releases/latest/download/fnm-macos.zip",
			fnmZip, 0644, w,
		); err != nil {
			return fmt.Errorf("fnm download: %w", err)
		}
		extractCmd := exec.Command("unzip", "-o", fnmZip, "fnm", "-d", binDir)
		extractCmd.Stdout = w
		extractCmd.Stderr = w
		if err := extractCmd.Run(); err != nil {
			return fmt.Errorf("fnm extract: %w", err)
		}
		os.Remove(fnmZip)
		os.Chmod(fnmPath, 0755) //nolint:errcheck
	}

	// mkcert
	mkcertPath := certs.MkcertPath()
	if _, err := os.Stat(mkcertPath); os.IsNotExist(err) {
		mkcertArch := "amd64"
		if arch == "arm64" {
			mkcertArch = "arm64"
		}
		mkcertURL := fmt.Sprintf(
			"https://github.com/FiloSottile/mkcert/releases/latest/download/mkcert-v1.4.4-darwin-%s",
			mkcertArch,
		)
		if err := downloadFile(mkcertURL, mkcertPath, 0755, w); err != nil {
			return fmt.Errorf("mkcert download: %w", err)
		}
	}

	return nil
}

// ensurePortForwarding installs the podman-mac-helper, which allows Podman
// Machine to bind to privileged ports (80, 443) as a regular user on macOS.
//
// The helper ships with Podman (typically at /opt/homebrew/bin/podman-mac-helper
// or /usr/local/bin/podman-mac-helper) and installs a system LaunchDaemon via
// `sudo podman-mac-helper install`.
//
// If the helper is not found or already installed, this is a no-op.
func ensurePortForwarding() error {
	// Check whether the LaunchDaemon is already installed.
	const daemonPlist = "/Library/LaunchDaemons/com.github.containers.podman.helper.plist"
	if _, err := os.Stat(daemonPlist); err == nil {
		return nil // already installed
	}

	// Locate the podman-mac-helper binary.
	helperPath, err := findPodmanMacHelper()
	if err != nil {
		fmt.Printf("    WARN: podman-mac-helper not found — ports 80/443 may not work.\n")
		fmt.Printf("    Install Podman via Homebrew and re-run 'lerd install'.\n")
		return nil // not fatal; containers still work on non-privileged ports
	}

	fmt.Println("  [sudo required] Installing podman-mac-helper for ports 80/443")
	cmd := exec.Command("sudo", helperPath, "install")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("    WARN: podman-mac-helper install: %v\n", err)
		fmt.Printf("    Ports 80/443 may not work — run manually: sudo %s install\n", helperPath)
	}
	return nil
}

// findPodmanMacHelper returns the path to podman-mac-helper if found.
func findPodmanMacHelper() (string, error) {
	// Try PATH first (covers non-Homebrew installs).
	if p, err := exec.LookPath("podman-mac-helper"); err == nil {
		return p, nil
	}
	// Common Homebrew locations.
	for _, candidate := range []string{
		"/opt/homebrew/bin/podman-mac-helper",
		"/usr/local/bin/podman-mac-helper",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("podman-mac-helper not found")
}

