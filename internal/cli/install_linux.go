//go:build linux

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

	// fnm
	fnmPath := filepath.Join(binDir, "fnm")
	if _, err := os.Stat(fnmPath); os.IsNotExist(err) {
		fnmZip := filepath.Join(binDir, "fnm-linux.zip")
		if err := downloadFile(
			"https://github.com/Schniz/fnm/releases/latest/download/fnm-linux.zip",
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
			"https://github.com/FiloSottile/mkcert/releases/latest/download/mkcert-v1.4.4-linux-%s",
			mkcertArch,
		)
		if err := downloadFile(mkcertURL, mkcertPath, 0755, w); err != nil {
			return fmt.Errorf("mkcert download: %w", err)
		}
	}

	return nil
}

// ensurePortForwarding is a no-op on Linux; ensureUnprivilegedPorts handles
// port 80/443 access via the ip_unprivileged_port_start sysctl.
func ensurePortForwarding() error { return nil }
