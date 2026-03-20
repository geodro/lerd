package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	lerdUpdate "github.com/geodro/lerd/internal/update"
	"github.com/spf13/cobra"
)

const githubRepo = "geodro/lerd"

// These vars are overridden in tests to point at an httptest server.
var (
	githubReleasesBase = "https://github.com/" + githubRepo + "/releases"
	githubDownloadBase = "https://github.com/" + githubRepo + "/releases/download"
)

// NewUpdateCmd returns the update command.
func NewUpdateCmd(currentVersion string) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update Lerd to the latest release",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUpdate(currentVersion)
		},
	}
}

func runUpdate(currentVersion string) error {
	fmt.Println("==> Checking for updates")

	latest, err := fetchLatestVersion()
	if err != nil {
		return fmt.Errorf("could not fetch latest version: %w", err)
	}

	cur := lerdUpdate.StripV(currentVersion)
	lat := lerdUpdate.StripV(latest)

	if cur == lat {
		fmt.Printf("  Already on latest: v%s\n", lat)
		return nil
	}

	fmt.Printf("  Current: v%s\n", cur)
	fmt.Printf("  Latest:  v%s\n", lat)

	self, err := selfPath()
	if err != nil {
		return err
	}

	fmt.Printf("  --> Downloading lerd v%s ... ", lat)
	extracted, cleanup, err := downloadReleaseBinary(latest)
	if err != nil {
		return err
	}
	defer cleanup()
	fmt.Println("OK")

	// Atomically replace lerd.
	tmp := self + ".tmp"
	if err := copyFile(filepath.Join(extracted, "lerd"), tmp, 0755); err != nil {
		return fmt.Errorf("writing update: %w", err)
	}
	if err := os.Rename(tmp, self); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replacing binary: %w", err)
	}

	// Also replace lerd-tray if it was included in this release.
	trayBin := filepath.Join(extracted, "lerd-tray")
	if _, err := os.Stat(trayBin); err == nil {
		selfTray := filepath.Join(filepath.Dir(self), "lerd-tray")
		tmpTray := selfTray + ".tmp"
		if err := copyFile(trayBin, tmpTray, 0755); err == nil {
			os.Rename(tmpTray, selfTray) //nolint:errcheck
		}
	}

	fmt.Printf("\nLerd updated to v%s — applying infrastructure changes...\n\n", lat)

	// Re-exec the new binary with `install` to reapply quadlet files,
	// DNS config, sysctl, etc. lerd install is idempotent.
	installCmd := exec.Command(self, "install")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	installCmd.Stdin = os.Stdin
	if err := installCmd.Run(); err != nil {
		return err
	}

	// Only rebuild PHP-FPM images if the embedded Containerfile changed.
	if podman.NeedsFPMRebuild() {
		fmt.Println("\n==> PHP-FPM Containerfile changed — rebuilding images")
		rebuildCmd := exec.Command(self, "php:rebuild")
		rebuildCmd.Stdout = os.Stdout
		rebuildCmd.Stderr = os.Stderr
		rebuildCmd.Stdin = os.Stdin
		if err := rebuildCmd.Run(); err != nil {
			return err
		}
	} else {
		fmt.Println("\n==> PHP-FPM images are up to date, skipping rebuild")
		// Ensure FPM containers are running after the install step.
		versions, _ := phpPkg.ListInstalled()
		for _, v := range versions {
			unit := "lerd-php" + strings.ReplaceAll(v, ".", "") + "-fpm"
			fmt.Printf("  --> %s ... ", unit)
			if err := podman.StartUnit(unit); err != nil {
				fmt.Printf("WARN (%v)\n", err)
			} else {
				fmt.Println("OK")
			}
		}
	}
	return nil
}

func fetchLatestVersion() (string, error) {
	url := githubReleasesBase + "/latest"
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodGet, url, nil) //nolint:noctx
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "lerd-cli")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		return "", fmt.Errorf("unexpected status from %s: HTTP %d", url, resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no Location header in redirect from %s", url)
	}
	parts := strings.Split(location, "/tag/")
	if len(parts) != 2 || parts[1] == "" {
		return "", fmt.Errorf("unexpected release URL format: %s", location)
	}
	return parts[1], nil
}

// downloadReleaseBinary downloads and extracts the release archive for the
// current platform. Returns the path to the extracted binary and a cleanup func.
// downloadReleaseBinary downloads and extracts the release archive for the
// current platform. Returns the path to the extracted directory and a cleanup func.
func downloadReleaseBinary(version string) (string, func(), error) {
	arch := runtime.GOARCH // "amd64" or "arm64"
	ver := stripV(version)

	filename := fmt.Sprintf("lerd_%s_linux_%s.tar.gz", ver, arch)
	url := fmt.Sprintf("%s/v%s/%s", githubDownloadBase, ver, filename)

	tmp, err := os.MkdirTemp("", "lerd-update-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(tmp) }

	archive := filepath.Join(tmp, filename)
	if err := downloadFile(url, archive, 0644, io.Discard); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("download failed (%s): %w", url, err)
	}

	cmd := exec.Command("tar", "-xzf", archive, "-C", tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("extract failed: %w\n%s", err, out)
	}

	if _, err := os.Stat(filepath.Join(tmp, "lerd")); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("binary not found in archive")
	}
	return tmp, cleanup, nil
}

func selfPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("could not determine executable path: %w", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return "", fmt.Errorf("could not resolve executable path: %w", err)
	}
	return self, nil
}

func copyFile(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func stripV(v string) string { return lerdUpdate.StripV(v) }
