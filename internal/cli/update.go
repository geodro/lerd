package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/feedback"
	"github.com/geodro/lerd/internal/origin"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	"github.com/geodro/lerd/internal/store"
	lerdUpdate "github.com/geodro/lerd/internal/update"
	"github.com/spf13/cobra"
)

// githubDownloadBases returns release-asset download bases in priority order,
// read live. Overridden in tests to point at an httptest server.
var githubDownloadBases = origin.ReleaseDownloadBases

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
	feedback.Begin()
	feedback.Line("checking for updates")

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
		feedback.Done("already on latest v" + cur)
		return nil
	}

	feedback.Note("current v" + cur + " · latest " + feedback.Val("v"+lat))

	// Show what's new between the current and latest version.
	feedback.Line("what's new")
	changelog, _ := lerdUpdate.FetchChangelog(cur, lat)
	if changelog != "" {
		for _, line := range strings.Split(changelog, "\n") {
			fmt.Println("  " + line)
		}
	} else {
		fmt.Printf("  %s/tag/v%s\n", origin.ReleaseBaseURLs()[0], lat)
	}

	// A Homebrew-managed binary lives under a Cellar prefix; self-replacing it
	// would fight `brew`, so defer to it. Curl-installed binaries (the default
	// on macOS now) live in ~/.local/bin and self-update like Linux does below.
	if runtime.GOOS == "darwin" {
		if self, err := selfPath(); err == nil && isHomebrewManaged(self) {
			fmt.Printf("\nThis is a Homebrew install. To update, run:\n\n  brew upgrade lerd\n\n")
			return nil
		}
	}

	// Ask for confirmation.
	if !feedback.Confirm("Update to v"+lat+"?", true) {
		feedback.Line("update cancelled")
		return nil
	}

	self, err := selfPath()
	if err != nil {
		return err
	}

	// Back up current binary for rollback.
	backupBinary(self, currentVersion)

	dl := feedback.Start("downloading lerd v" + lat)
	extracted, cleanup, err := downloadReleaseBinary(latest)
	if err != nil {
		dl.Fail(err)
		return err
	}
	defer cleanup()
	dl.OK("")

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

	feedback.Done("lerd updated to v" + lat)
	feedback.Line("applying infrastructure changes")
	fmt.Println()

	// Re-exec the new binary with `install` to reapply quadlet files,
	// DNS config, sysctl, etc. lerd install is idempotent. Pass
	// --from-update so the install pass honours the saved DNS choice
	// silently instead of re-prompting the user.
	installCmd := exec.Command(self, "install", "--from-update")
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
		if feedback.Confirm("MinIO detected — migrate to RustFS?", false) {
			if err := runMinioMigrate(nil, nil); err != nil {
				feedback.Warn("migration failed: %v", err)
			}
		}
	}

	// FPM rebuild + container starts now happen inside `lerd install`
	// (gated on autostart), so we don't repeat them here.

	restartLerdUserServices()
	return nil
}

// refreshStoreFrameworks re-fetches every cached framework yaml so users pick
// up schema additions (per_worktree, etc.) without waiting for the 24h
// staleness check in GetFrameworkForDir to expire.
func refreshStoreFrameworks() {
	dir := config.StoreFrameworksDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type target struct{ name, version string }
	var targets []target
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".yaml")
		var t target
		if at := strings.Index(base, "@"); at != -1 {
			t.name = base[:at]
			t.version = base[at+1:]
		} else {
			t.name = base
		}
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		return
	}
	feedback.Header(fmt.Sprintf("Refreshing %d framework%s", len(targets), pluralS(len(targets))))
	client := store.NewClient()
	for _, t := range targets {
		label := t.name
		if t.version != "" {
			label = t.name + "@" + t.version
		}
		step := feedback.Start(label)
		fw, err := client.FetchFramework(t.name, t.version)
		if err != nil {
			step.Fail(err)
			continue
		}
		if err := config.SaveStoreFramework(fw); err != nil {
			step.Fail(err)
			continue
		}
		step.OK("")
	}
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
	feedback.Header("Refreshing global AI skills")
	if err := RefreshGlobalAISkills(home, true); err != nil {
		feedback.Warn("could not refresh global AI skills: %v", err)
	}
	if !IsMCPGloballyRegistered() {
		feedback.Note("re-registering lerd with Claude Code (was missing)")
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

	feedback.Header(fmt.Sprintf("Refreshing project AI skills (%d)", len(opted)))
	for _, p := range opted {
		s := feedback.Start(p)
		if err := RefreshProjectAISkills(p, false); err != nil {
			s.Fail(err)
			continue
		}
		s.OK("")
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

// restartLerdUserServices restarts the long-running lerd user units (systemd on
// Linux, launchd on macOS) so they pick up the freshly replaced binary. Both
// keep the old executable alive for processes that already have it open, so
// without an explicit restart the daemons keep running the pre-update code and
// report the old version. Only currently-active units are restarted, so
// disabled services are left alone.
func restartLerdUserServices() {
	units := []string{"lerd-ui", "lerd-watcher", "lerd-tray"}
	var active []string
	for _, u := range units {
		if services.Mgr.IsActive(u) {
			active = append(active, u)
		}
	}
	if len(active) == 0 {
		return
	}
	feedback.Header("Restarting lerd services to pick up the new binary")
	for _, u := range active {
		s := feedback.Start(u)
		if err := services.Mgr.Restart(u); err != nil {
			// Best-effort: the binary swap already succeeded, so a restart that
			// didn't take is a warning, not a failure of the update itself.
			s.Warn(err)
			continue
		}
		s.OK("")
	}
}

// downloadReleaseBinary downloads and extracts the release archive for the
// current platform. Returns the path to the extracted binary and a cleanup func.
// downloadReleaseBinary downloads and extracts the release archive for the
// current platform. Returns the path to the extracted directory and a cleanup func.
func downloadReleaseBinary(version string) (string, func(), error) {
	arch := runtime.GOARCH // "amd64" or "arm64"
	ver := stripV(version)

	filename := fmt.Sprintf("lerd_%s_%s_%s.tar.gz", ver, runtime.GOOS, arch)

	tmp, err := os.MkdirTemp("", "lerd-update-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(tmp) }

	archive := filepath.Join(tmp, filename)
	if err := downloadArchive(ver, filename, archive); err != nil {
		cleanup()
		return "", func() {}, err
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

// downloadArchive fetches the release archive, trying each download base in
// order until one succeeds, and returns an aggregated error if none do.
func downloadArchive(ver, filename, archive string) error {
	var errs []string
	for _, base := range githubDownloadBases() {
		url := fmt.Sprintf("%s/v%s/%s", base, ver, filename)
		if err := downloadFile(url, archive, 0644, io.Discard); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", url, err))
			continue
		}
		origin.NoteFetched(base)
		return nil
	}
	return fmt.Errorf("download failed: %s", strings.Join(errs, "; "))
}

// isHomebrewManaged reports whether the resolved binary path lives inside a
// Homebrew Cellar, in which case `lerd update` defers to `brew upgrade` rather
// than self-replacing files brew owns.
func isHomebrewManaged(path string) bool {
	return strings.Contains(path, "/Cellar/")
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
		feedback.Warn("could not back up binary for rollback: %v", err)
		return
	}

	// Back up lerd-tray if it exists next to the main binary.
	trayPath := filepath.Join(filepath.Dir(self), "lerd-tray")
	if _, err := os.Stat(trayPath); err == nil {
		if err := copyFile(trayPath, config.BackupTrayFile(), 0755); err != nil {
			feedback.Warn("could not back up lerd-tray: %v", err)
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

	feedback.Header(fmt.Sprintf("Rolling back to v%s", prevVersion))

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
	feedback.Line("Resetting lerd network for rollback")
	if attached, _, err := podman.RecreateNetwork("lerd", nil); err == nil {
		for _, c := range attached {
			_ = podman.StartUnit(c)
		}
	}

	// Old install daemon-reloads before WriteServiceUnit, so a leftover
	// Type=notify on disk pins the cache and the post-write Restart blocks
	// on sd_notify(READY=1) which the old binary never sends. Strip it so
	// the cache picks up Type=simple immediately.
	prepUserUnitsForRollback("lerd-ui.service", "lerd-watcher.service")

	feedback.Note(fmt.Sprintf("Rolled back to v%s, applying infrastructure changes...", prevVersion))

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
