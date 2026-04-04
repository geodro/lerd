package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	lerdUpdate "github.com/geodro/lerd/internal/update"
	"github.com/spf13/cobra"
)

const githubRepo = "geodro/lerd"

// These vars are overridden in tests to point at an httptest server.
var (
	githubDownloadBase = "https://github.com/" + githubRepo + "/releases/download"
)

// NewUpdateCmd returns the update command.
func NewUpdateCmd(currentVersion string) *cobra.Command {
	var beta, rollback bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update Lerd to the latest release",
		RunE: func(_ *cobra.Command, _ []string) error {
			if rollback {
				return runRollback()
			}
			return runUpdate(currentVersion, beta)
		},
	}
	cmd.Flags().BoolVar(&beta, "beta", false, "Update to the latest pre-release build")
	cmd.Flags().BoolVar(&rollback, "rollback", false, "Revert to the previously installed version")
	cmd.MarkFlagsMutuallyExclusive("beta", "rollback")
	return cmd
}

func runUpdate(currentVersion string, beta bool) error {
	if runtime.GOOS == "darwin" {
		fmt.Println("Lerd on macOS is managed by Homebrew.")
		fmt.Println("To upgrade, run:  brew upgrade lerd")
		return nil
	}

	fmt.Println("==> Checking for updates")

	var latest string
	var err error
	if beta {
		latest, err = lerdUpdate.FetchLatestPrerelease()
		if err != nil {
			return fmt.Errorf("could not fetch latest pre-release: %w", err)
		}
	} else {
		latest, err = lerdUpdate.FetchLatestVersion()
		if err != nil {
			return fmt.Errorf("could not fetch latest version: %w", err)
		}
	}

	// Strip "v" prefix and any git-describe suffix (e.g. "-dirty", "-5-gabcdef")
	// so local dev builds compare cleanly against release tags. Preserve semver
	// pre-release suffixes like "-beta.1".
	cur := stripGitDescribe(lerdUpdate.StripV(currentVersion))
	lat := lerdUpdate.StripV(latest)

	if !lerdUpdate.VersionGreaterThan(lat, cur) {
		fmt.Printf("  Already on latest: v%s\n", cur)
		return nil
	}

	fmt.Printf("  Current: v%s\n", cur)
	fmt.Printf("  Latest:  v%s\n", lat)

	// Show what's new between the current and latest version.
	fmt.Println("\n==> What's new")
	changelog, _ := lerdUpdate.FetchChangelog(cur, lat)
	if changelog != "" {
		for _, line := range strings.Split(changelog, "\n") {
			fmt.Println("  " + line)
		}
	} else {
		fmt.Printf("  https://github.com/%s/releases/tag/v%s\n", githubRepo, lat)
	}

	// Ask for confirmation.
	fmt.Printf("\nUpdate to v%s? [y/N] ", lat)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Println("Update cancelled.")
		return nil
	}

	self, err := selfPath()
	if err != nil {
		return err
	}

	// Back up current binary for rollback.
	backupBinary(self, currentVersion)

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

	// Update the cache so lerd status / doctor stop showing a stale notice.
	lerdUpdate.WriteUpdateCache(lat)

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

	// Offer MinIO → RustFS migration if legacy data directory exists and the
	// minio container is still running (skip if already migrated to RustFS).
	minioRunning, _ := podman.ContainerRunning("lerd-minio")
	if _, err := os.Stat(config.DataSubDir("minio")); err == nil && minioRunning {
		fmt.Print("\n==> MinIO detected — migrate to RustFS? [y/N] ")
		migrateReader := bufio.NewReader(os.Stdin)
		migrateAnswer, _ := migrateReader.ReadString('\n')
		migrateAnswer = strings.TrimSpace(strings.ToLower(migrateAnswer))
		if migrateAnswer == "y" || migrateAnswer == "yes" {
			if err := runMinioMigrate(nil, nil); err != nil {
				fmt.Fprintf(os.Stderr, "  warn: migration failed: %v\n", err)
			}
		}
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
			if err := services.Mgr.Start(unit); err != nil {
				fmt.Printf("WARN (%v)\n", err)
			} else {
				fmt.Println("OK")
			}
		}
	}
	return nil
}

// downloadReleaseBinary downloads and extracts the release archive for the
// current platform. Returns the path to the extracted binary and a cleanup func.
// downloadReleaseBinary downloads and extracts the release archive for the
// current platform. Returns the path to the extracted directory and a cleanup func.
func downloadReleaseBinary(version string) (string, func(), error) {
	arch := runtime.GOARCH // "amd64" or "arm64"
	ver := stripV(version)

	filename := fmt.Sprintf("lerd_%s_%s_%s.tar.gz", ver, runtime.GOOS, arch)
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

// stripGitDescribe removes git-describe suffixes like "-dirty" or "-5-gabcdef"
// while preserving semver pre-release tags like "-beta.1" or "-rc.1".
// Git-describe suffixes contain a commit hash segment starting with "g".
func stripGitDescribe(v string) string {
	for {
		i := strings.LastIndexByte(v, '-')
		if i < 0 {
			break
		}
		suffix := v[i+1:]
		if suffix == "dirty" {
			v = v[:i]
			continue
		}
		// Git describe hash segment: g followed by hex chars.
		// Also strip the preceding commit-count segment (e.g. "-5-gabcdef").
		if len(suffix) > 1 && suffix[0] == 'g' && isHex(suffix[1:]) {
			v = v[:i]
			// Now check if the new last segment is a numeric commit count.
			if j := strings.LastIndexByte(v, '-'); j >= 0 && isNumeric(v[j+1:]) {
				v = v[:j]
			}
			continue
		}
		break
	}
	return v
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s) > 0
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

// backupBinary copies the current binary and version to backup locations for rollback.
func backupBinary(self, currentVersion string) {
	if err := copyFile(self, config.BackupBinaryFile(), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "  warn: could not back up binary for rollback: %v\n", err)
		return
	}

	// Back up lerd-tray if it exists next to the main binary.
	trayPath := filepath.Join(filepath.Dir(self), "lerd-tray")
	if _, err := os.Stat(trayPath); err == nil {
		if err := copyFile(trayPath, config.BackupTrayFile(), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "  warn: could not back up lerd-tray: %v\n", err)
		}
	}

	os.WriteFile(config.BackupVersionFile(), []byte(lerdUpdate.StripV(currentVersion)), 0644) //nolint:errcheck
}

// runRollback restores the previously backed-up binary.
func runRollback() error {
	bakPath := config.BackupBinaryFile()
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		return fmt.Errorf("no backup found — rollback is only available after a successful update")
	}

	prevVersion := "unknown"
	if data, err := os.ReadFile(config.BackupVersionFile()); err == nil {
		prevVersion = strings.TrimSpace(string(data))
	}

	self, err := selfPath()
	if err != nil {
		return err
	}

	fmt.Printf("==> Rolling back to v%s\n", prevVersion)

	// Atomically replace lerd.
	tmp := self + ".tmp"
	if err := copyFile(bakPath, tmp, 0755); err != nil {
		return fmt.Errorf("restoring backup: %w", err)
	}
	if err := os.Rename(tmp, self); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replacing binary: %w", err)
	}

	// Restore lerd-tray if a backup exists.
	trayBak := config.BackupTrayFile()
	if _, err := os.Stat(trayBak); err == nil {
		selfTray := filepath.Join(filepath.Dir(self), "lerd-tray")
		tmpTray := selfTray + ".tmp"
		if err := copyFile(trayBak, tmpTray, 0755); err == nil {
			os.Rename(tmpTray, selfTray) //nolint:errcheck
		}
	}

	// Remove backup files so you can't double-rollback.
	os.Remove(bakPath)
	os.Remove(config.BackupTrayFile())
	os.Remove(config.BackupVersionFile())

	// Update the cache.
	lerdUpdate.WriteUpdateCache(prevVersion)

	fmt.Printf("\nRolled back to v%s — applying infrastructure changes...\n\n", prevVersion)

	// Re-exec the new binary with `install`, same as a normal update.
	installCmd := exec.Command(self, "install")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	installCmd.Stdin = os.Stdin
	return installCmd.Run()
}
