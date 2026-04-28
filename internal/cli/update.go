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
	"github.com/geodro/lerd/internal/systemd"
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
				if runtime.GOOS == "darwin" {
					return fmt.Errorf("rollback is not supported on macOS — use 'brew switch lerd <version>' instead")
				}
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
	cur := lerdUpdate.StripGitDescribe(lerdUpdate.StripV(currentVersion))
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

	// On macOS, Homebrew manages the binary — instruct the user rather than
	// attempting a self-replace which would overwrite Homebrew's managed files.
	if runtime.GOOS == "darwin" {
		fmt.Printf("\nTo update, run:\n\n  brew upgrade lerd\n\n")
		return nil
	}

	// Ask for confirmation.
	fmt.Printf("\nUpdate to v%s? [Y/n] ", lat)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "n" || answer == "no" {
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

	refreshGlobalMCPSkills()
	refreshProjectMCPSkills()

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
			if err := podman.StartUnit(unit); err != nil {
				fmt.Printf("WARN (%v)\n", err)
			} else {
				fmt.Println("OK")
			}
		}
	}

	restartLerdUserServices()
	return nil
}

// refreshGlobalMCPSkills re-writes the user-scope skill, rules, and guidelines
// files when lerd MCP is registered globally, so the AI's description of
// available tools stays aligned with the newly installed binary. Also heals
// the Claude Code MCP registration: an install after an uninstall (or a
// Claude config migration) can lose the `claude mcp add` entry while the
// marker files remain; re-run the idempotent add so lerd shows up again.
func refreshGlobalMCPSkills() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if !mcpEnabledGlobally(home) {
		return
	}
	fmt.Println("\n==> Refreshing global MCP skills and guidelines")
	if err := WriteGlobalAISkills(home, true); err != nil {
		fmt.Fprintf(os.Stderr, "  warn: could not refresh global AI skills: %v\n", err)
	}
	if !IsMCPGloballyRegistered() {
		fmt.Println("  Re-registering lerd with Claude Code (was missing)")
		ensureClaudeMCPRegistered()
	}
}

// refreshProjectMCPSkills re-writes per-project AI artefacts for every opted-in
// project (registered site or park subdir with a lerd marker). Projects whose
// content already matches stay untouched.
func refreshProjectMCPSkills() {
	paths := gatherProjectPaths()
	if len(paths) == 0 {
		return
	}

	opted := make([]string, 0, len(paths))
	for _, p := range paths {
		if ProjectHasLerdSkills(p) {
			opted = append(opted, p)
		}
	}
	if len(opted) == 0 {
		return
	}

	fmt.Printf("\n==> Refreshing project MCP skills (%d project%s)\n", len(opted), pluralS(len(opted)))
	for _, p := range opted {
		if err := WriteProjectAISkills(p, false); err != nil {
			fmt.Fprintf(os.Stderr, "  warn: %s: %v\n", p, err)
			continue
		}
		fmt.Printf("  refreshed %s\n", p)
	}
}

// gatherProjectPaths lists registered sites plus immediate subdirs of parks.
// The park scan covers projects that were injected but never registered as
// lerd sites (e.g. non-PHP projects the user added by hand).
func gatherProjectPaths() []string {
	seen := make(map[string]struct{})
	add := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		seen[abs] = struct{}{}
	}

	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			add(s.Path)
		}
	}

	if cfg, err := config.LoadGlobal(); err == nil {
		for _, park := range cfg.ParkedDirectories {
			if park == "" {
				continue
			}
			parkAbs, err := filepath.Abs(park)
			if err != nil {
				continue
			}
			entries, err := os.ReadDir(parkAbs)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				add(filepath.Join(parkAbs, e.Name()))
			}
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// mcpEnabledGlobally reports whether the user opted into global MCP at some
// point. Checks (a) Claude Code user-scope registration and (b) the lerd-owned
// marker files written by mcp:enable-global. The marker check lets us detect
// users who enabled globally without Claude Code (Cursor-only, Junie-only) and
// users whose `claude` CLI is temporarily unavailable.
func mcpEnabledGlobally(home string) bool {
	if IsMCPGloballyRegistered() {
		return true
	}
	markers := []string{
		filepath.Join(home, ".claude", "skills", "lerd", "SKILL.md"),
		filepath.Join(home, ".cursor", "rules", "lerd.mdc"),
	}
	for _, p := range markers {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// restartLerdUserServices restarts the long-running lerd user units so they
// pick up the freshly replaced binary. Linux keeps the old inode alive for
// processes that have the binary open, so without an explicit restart the
// daemons keep executing the pre-update code and report the old version.
// Only currently-active units are restarted, so disabled services are left
// alone.
func restartLerdUserServices() {
	units := []string{"lerd-ui.service", "lerd-watcher.service", "lerd-tray.service"}
	var active []string
	for _, u := range units {
		if systemd.IsServiceActive(u) {
			active = append(active, u)
		}
	}
	if len(active) == 0 {
		return
	}
	fmt.Println("\n==> Restarting lerd services to pick up the new binary")
	for _, u := range active {
		fmt.Printf("  --> %s ... ", u)
		if err := systemd.RestartService(u); err != nil {
			fmt.Printf("WARN (%v)\n", err)
		} else {
			fmt.Println("OK")
		}
	}
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

	cmd := exec.Command("tar", "--no-same-owner", "-xzf", archive, "-C", tmp)
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

	// Recreate the network cleanly so the rolled-back binary's
	// `lerd install` starts from a known-good state. The current
	// binary's probe logic decides v4-only vs dual-stack; the old
	// binary's EnsureNetwork will accept whatever schema it finds.
	fmt.Println("  --> Resetting lerd network for rollback")
	if attached, _, err := podman.RecreateNetwork("lerd"); err == nil {
		for _, c := range attached {
			_ = podman.StartUnit(c)
		}
	}

	// Old install daemon-reloads before WriteServiceUnit, so a leftover
	// Type=notify on disk pins the cache and the post-write Restart blocks
	// on sd_notify(READY=1) which the old binary never sends. Strip it so
	// the cache picks up Type=simple immediately.
	prepUserUnitsForRollback("lerd-ui.service", "lerd-watcher.service")

	fmt.Printf("\nRolled back to v%s — applying infrastructure changes...\n\n", prevVersion)

	// Re-exec the new binary with `install`, same as a normal update.
	installCmd := exec.Command(self, "install")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	installCmd.Stdin = os.Stdin
	return installCmd.Run()
}

// prepUserUnitsForRollback strips any "Type=notify" line from the named
// systemd user unit files on disk so the rolled-back binary's install
// flow does not restart them under a notify cache the old binary cannot
// satisfy.
func prepUserUnitsForRollback(units ...string) {
	for _, name := range units {
		path := filepath.Join(config.SystemdUserDir(), name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		stripped := stripTypeNotify(string(data))
		if string(data) == stripped {
			continue
		}
		_ = os.WriteFile(path, []byte(stripped), 0644)
	}
}

// stripTypeNotify removes any line whose trimmed content is exactly
// "Type=notify" from a systemd unit file body.
func stripTypeNotify(content string) string {
	lines := strings.Split(content, "\n")
	out := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) == "Type=notify" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
