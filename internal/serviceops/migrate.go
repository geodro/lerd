package serviceops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// migratorFn drives a one-shot version migration for a single service family.
// Implementations dump current data, swap the data dir aside, switch image,
// restart, and restore the dump. Backups land in config.BackupsDir().
type migratorFn func(name, targetImage string, emit func(PhaseEvent)) error

// migrators is restricted to families with stable text-format dumps that load
// cleanly across versions (mysqldump / pg_dumpall). Engines whose dumps are
// version-specific binary blobs need manual upgrades.
var migrators = map[string]migratorFn{
	"mysql":    migrateMysql,
	"mariadb":  migrateMysql,
	"postgres": migratePostgres,
}

// dumpRestoreTimeout caps any single in-container dump or restore exec so a
// wedged container can't block the migrate forever.
const dumpRestoreTimeout = 30 * time.Minute

// SupportsMigration reports whether a registered family migrator exists for
// the named service.
func SupportsMigration(name string) bool {
	_, ok := migrators[familyOf(name)]
	return ok
}

// MigrateService runs a per-family dump/restore migration so the service can
// move across data-incompatible SQL versions (e.g. mysql 8.0 → 9.0). Errors
// when no handler is registered for the service family.
func MigrateService(name, targetImage string, emit func(PhaseEvent)) error {
	unlock := lockService(name)
	defer unlock()

	fam := familyOf(name)
	fn, ok := migrators[fam]
	if !ok {
		return fmt.Errorf("no migration handler for %s (family=%q) — dump and restore manually following the service's docs", name, fam)
	}
	if targetImage == "" {
		return fmt.Errorf("targetImage is required")
	}
	if err := os.MkdirAll(config.BackupsDir(), 0700); err != nil {
		return fmt.Errorf("creating backups dir: %w", err)
	}
	return fn(name, targetImage, emit)
}

// familyOf returns the service family for a default preset or installed
// custom service.
func familyOf(name string) string {
	if config.IsDefaultPreset(name) {
		if p, err := config.LoadPreset(name); err == nil {
			return p.Family
		}
	}
	return config.FamilyOfName(name)
}

// presetForService returns the bundled preset a service was installed from, or
// nil when it has none or cannot be resolved. A nil result makes the migrate
// target fall back to literal-tag substitution on the current image.
func presetForService(name string) *config.Preset {
	if config.IsDefaultPreset(name) {
		p, _ := config.LoadPreset(name)
		return p
	}
	svc, err := config.LoadCustomService(name)
	if err != nil || svc.Preset == "" {
		return nil
	}
	p, _ := config.LoadPreset(svc.Preset)
	return p
}

// ResolveMigrateTarget maps a migrate version argument to a target image. A
// bare preset version ("18") resolves to that version's full preset image; an
// unknown argument is substituted as a literal tag onto the current image.
func ResolveMigrateTarget(name, currentImage, version string) (string, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("a target version is required")
	}
	return migrateTargetImage(presetForService(name), currentImage, version)
}

// migrateTargetImage is the pure resolution behind ResolveMigrateTarget, split
// out so the rules can be tested without an installed service on disk.
func migrateTargetImage(p *config.Preset, currentImage, version string) (string, error) {
	if p != nil && len(p.Versions) > 0 {
		if svc, err := p.Resolve(version); err == nil {
			p.ApplyPlatformOverride(svc, runtime.GOOS)
			return svc.Image, nil
		}
	}
	if currentImage == "" {
		return "", fmt.Errorf("no preset version %q and no current image to derive a tag from", version)
	}
	if at := strings.LastIndex(currentImage, ":"); at > 0 {
		return currentImage[:at] + ":" + version, nil
	}
	return currentImage + ":" + version, nil
}

func timestamped() string { return time.Now().UTC().Format("20060102-150405") }

// containerExec runs shellCmd inside a running container with a hard timeout.
// envPairs are passed via the exec env (not argv) so secrets don't leak into
// /proc/<pid>/cmdline. Captured output includes stderr.
func containerExec(container, shellCmd string, envPairs []string, stdin *os.File, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{"exec", "-i"}
	for _, kv := range envPairs {
		args = append(args, "--env", kv)
	}
	args = append(args, container, "sh", "-c", shellCmd)
	cmd := exec.CommandContext(ctx, podman.PodmanBin(), args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	return cmd.CombinedOutput()
}

// dumpToHost streams the output of a container command into a host file with
// 0600 permissions. envPairs go through podman exec --env so secrets stay out
// of argv.
func dumpToHost(container, shellCmd string, envPairs []string, hostPath string, timeout time.Duration) error {
	out, err := os.OpenFile(hostPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("creating dump file %s: %w", hostPath, err)
	}
	defer out.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{"exec"}
	for _, kv := range envPairs {
		args = append(args, "--env", kv)
	}
	args = append(args, container, "sh", "-c", shellCmd)
	cmd := exec.CommandContext(ctx, podman.PodmanBin(), args...)
	if err := runStreaming(cmd, out); err != nil {
		return fmt.Errorf("dump command failed: %w", err)
	}
	return nil
}

// runStreaming runs cmd with stdout written to out and stderr captured for
// error context. CombinedOutput refuses to run once Stdout is set, so the dump
// path has to wire the two streams explicitly instead.
func runStreaming(cmd *exec.Cmd, out *os.File) error {
	var stderr bytes.Buffer
	cmd.Stdout = out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\n%s", err, stderr.String())
	}
	return nil
}

// restoreFromHost streams a host file into a container command's stdin.
func restoreFromHost(container, shellCmd string, envPairs []string, hostPath string, timeout time.Duration) error {
	in, err := os.Open(hostPath)
	if err != nil {
		return fmt.Errorf("opening dump file %s: %w", hostPath, err)
	}
	defer in.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{"exec", "-i"}
	for _, kv := range envPairs {
		args = append(args, "--env", kv)
	}
	args = append(args, container, "sh", "-c", shellCmd)
	cmd := exec.CommandContext(ctx, podman.PodmanBin(), args...)
	cmd.Stdin = in
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restore command failed: %w\n%s", err, string(out))
	}
	return nil
}

// swapDataDirAside moves the current data dir to a timestamped backup name so
// the new container starts empty. Returns the backup path on success; empty
// string with no error means there was no data dir to swap.
func swapDataDirAside(svcName string) (string, error) {
	src := config.DataSubDir(svcName)
	if _, err := os.Stat(src); err != nil {
		return "", nil
	}
	dst := src + ".pre-migrate-" + timestamped()
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("moving data dir aside: %w", err)
	}
	if err := os.MkdirAll(src, 0755); err != nil {
		_ = os.Rename(dst, src)
		return "", fmt.Errorf("creating fresh data dir: %w", err)
	}
	return dst, nil
}

// restoreDataDirFromBackup undoes swapDataDirAside, used when migration fails
// before the new image is committed. Errors are bubbled — silent failure here
// would leave the data destroyed without operator awareness.
func restoreDataDirFromBackup(svcName, backupPath string) error {
	if backupPath == "" {
		return nil
	}
	src := config.DataSubDir(svcName)
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("clearing fresh data dir: %w", err)
	}
	if err := os.Rename(backupPath, src); err != nil {
		return fmt.Errorf("restoring data dir from %s: %w", backupPath, err)
	}
	return nil
}

// switchToTargetImage persists the new image, regenerates the quadlet, and
// restarts the unit.
func switchToTargetImage(name, targetImage string, emit func(PhaseEvent)) error {
	emit(PhaseEvent{Phase: "writing_quadlet", Image: targetImage})
	if err := persistImageChoice(name, targetImage, "migrate"); err != nil {
		return err
	}
	unit := "lerd-" + name
	emit(PhaseEvent{Phase: "restarting_unit", Unit: unit})
	return restartWithRetry(unit)
}

// waitContainerReady polls a service-specific readiness probe until it
// succeeds or timeout elapses.
func waitContainerReady(container, probeCmd string, envPairs []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := containerExec(container, probeCmd, envPairs, nil, 5*time.Second); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("container %s never became ready within %s", container, timeout)
}

// serviceConfigSnapshot captures a service's pre-migrate config so a failed
// image switch can be fully reverted instead of left half-applied.
type serviceConfigSnapshot struct {
	defaultPreset bool
	entry         config.ServiceConfig
	custom        *config.CustomService
}

// captureServiceConfig snapshots the on-disk service config before a migrate
// mutates it.
func captureServiceConfig(name string) (*serviceConfigSnapshot, error) {
	if config.IsDefaultPreset(name) {
		cfg, err := config.LoadGlobal()
		if err != nil {
			return nil, err
		}
		return &serviceConfigSnapshot{defaultPreset: true, entry: cfg.Services[name]}, nil
	}
	svc, err := config.LoadCustomService(name)
	if err != nil {
		return nil, err
	}
	return &serviceConfigSnapshot{custom: svc}, nil
}

// restoreConfig writes the snapshot back. It skips the quadlet so the config
// revert stays unit-testable; restoreQuadlet regenerates the unit file.
func (s *serviceConfigSnapshot) restoreConfig(name string) error {
	invalidateUpdateAvailability(name)
	if s.defaultPreset {
		cfg, err := config.LoadGlobal()
		if err != nil {
			return err
		}
		cfg.Services[name] = s.entry
		return config.SaveGlobal(cfg)
	}
	if s.custom == nil {
		return nil
	}
	return config.SaveCustomService(s.custom)
}

// restoreQuadlet regenerates the unit file from the restored config.
func (s *serviceConfigSnapshot) restoreQuadlet(name string) error {
	if s.defaultPreset {
		return EnsureDefaultPresetQuadlet(name)
	}
	if s.custom == nil {
		return nil
	}
	return EnsureCustomServiceQuadlet(s.custom)
}

// abortMigrate is the shared post-failure recovery: restore the old data dir,
// revert a persisted image switch via the snapshot, and bring the unit back up,
// joining errors so no fix is silent.
func abortMigrate(unit, name, backup string, snapshot *serviceConfigSnapshot, cause error) error {
	parts := []string{cause.Error()}
	if backup != "" {
		if err := restoreDataDirFromBackup(name, backup); err != nil {
			parts = append(parts, "data-dir restore failed: "+err.Error()+" (backup left at "+backup+")")
		}
	}
	if snapshot != nil {
		if err := snapshot.restoreConfig(name); err != nil {
			parts = append(parts, "reverting service config failed: "+err.Error())
		}
		if err := snapshot.restoreQuadlet(name); err != nil {
			parts = append(parts, "reverting quadlet failed: "+err.Error())
		}
	}
	if err := podman.StartUnit(unit); err != nil {
		parts = append(parts, "restarting unit failed: "+err.Error())
	}
	return fmt.Errorf("%s", strings.Join(parts, "; "))
}

// ---- mysql / mariadb -------------------------------------------------------

func migrateMysql(name, targetImage string, emit func(PhaseEvent)) error {
	unit := "lerd-" + name
	snapshot, err := captureServiceConfig(name)
	if err != nil {
		return fmt.Errorf("reading service config: %w", err)
	}
	dump := filepath.Join(config.BackupsDir(), name+"-"+timestamped()+".sql")
	rootEnv := []string{"MYSQL_PWD=lerd"}

	emit(PhaseEvent{Phase: "dumping_data", Message: "mysqldump → " + dump})
	dumpCmd := "mysqldump -uroot --all-databases --single-transaction --routines --triggers --events --quick"
	if err := dumpToHost(unit, dumpCmd, rootEnv, dump, dumpRestoreTimeout); err != nil {
		return fmt.Errorf("mysqldump: %w", err)
	}

	emit(PhaseEvent{Phase: "stopping_unit", Unit: unit})
	if err := podman.StopUnit(unit); err != nil {
		return fmt.Errorf("stopping %s: %w", unit, err)
	}

	emit(PhaseEvent{Phase: "swapping_data_dir", Message: "old data preserved alongside the dump"})
	backup, err := swapDataDirAside(name)
	if err != nil {
		return abortMigrate(unit, name, "", nil, err)
	}

	emit(PhaseEvent{Phase: "pulling_image", Image: targetImage})
	if err := podman.PullImageWithProgress(targetImage, func(line string) {
		emit(PhaseEvent{Phase: "pulling_image", Message: line})
	}); err != nil {
		return abortMigrate(unit, name, backup, nil, fmt.Errorf("pulling target: %w", err))
	}

	if err := switchToTargetImage(name, targetImage, emit); err != nil {
		return abortMigrate(unit, name, backup, snapshot, err)
	}

	emit(PhaseEvent{Phase: "waiting_ready", Unit: unit})
	probe := "mysql -uroot -e 'SELECT 1' >/dev/null 2>&1 || mariadb -uroot -e 'SELECT 1' >/dev/null 2>&1"
	if err := waitContainerReady(unit, probe, rootEnv, 90*time.Second); err != nil {
		return fmt.Errorf("%w. Dump preserved at %s; old data dir at %s", err, dump, backup)
	}

	emit(PhaseEvent{Phase: "restoring_data", Message: dump})
	restoreCmd := "mysql -uroot 2>&1 || mariadb -uroot 2>&1"
	if err := restoreFromHost(unit, restoreCmd, rootEnv, dump, dumpRestoreTimeout); err != nil {
		return fmt.Errorf("restore: %w. Dump preserved at %s; old data dir at %s", err, dump, backup)
	}

	if err := recordMigrateBackup(name, backup); err != nil {
		return fmt.Errorf("recording migrate backup: %w. Old data dir kept at %s", err, backup)
	}
	emit(PhaseEvent{Phase: "done", Image: targetImage, Unit: unit, Message: "Migrated. Old data dir kept at " + backup + "; remove when verified."})
	return nil
}

// ---- postgres --------------------------------------------------------------

func migratePostgres(name, targetImage string, emit func(PhaseEvent)) error {
	unit := "lerd-" + name
	snapshot, err := captureServiceConfig(name)
	if err != nil {
		return fmt.Errorf("reading service config: %w", err)
	}
	dump := filepath.Join(config.BackupsDir(), name+"-"+timestamped()+".sql")
	pgEnv := []string{"PGPASSWORD=lerd"}

	emit(PhaseEvent{Phase: "dumping_data", Message: "pg_dumpall → " + dump})
	dumpCmd := "pg_dumpall -h 127.0.0.1 -U postgres --clean --if-exists"
	if err := dumpToHost(unit, dumpCmd, pgEnv, dump, dumpRestoreTimeout); err != nil {
		return fmt.Errorf("pg_dumpall: %w", err)
	}

	emit(PhaseEvent{Phase: "stopping_unit", Unit: unit})
	if err := podman.StopUnit(unit); err != nil {
		return fmt.Errorf("stopping %s: %w", unit, err)
	}

	emit(PhaseEvent{Phase: "swapping_data_dir"})
	backup, err := swapDataDirAside(name)
	if err != nil {
		return abortMigrate(unit, name, "", nil, err)
	}

	emit(PhaseEvent{Phase: "pulling_image", Image: targetImage})
	if err := podman.PullImageWithProgress(targetImage, func(line string) {
		emit(PhaseEvent{Phase: "pulling_image", Message: line})
	}); err != nil {
		return abortMigrate(unit, name, backup, nil, fmt.Errorf("pulling target: %w", err))
	}

	if err := switchToTargetImage(name, targetImage, emit); err != nil {
		return abortMigrate(unit, name, backup, snapshot, err)
	}

	emit(PhaseEvent{Phase: "waiting_ready", Unit: unit})
	probe := "psql -h 127.0.0.1 -U postgres -c 'SELECT 1' >/dev/null 2>&1"
	if err := waitContainerReady(unit, probe, pgEnv, 90*time.Second); err != nil {
		return fmt.Errorf("%w. Dump preserved at %s; old data dir at %s", err, dump, backup)
	}

	emit(PhaseEvent{Phase: "restoring_data", Message: dump})
	restoreCmd := "psql -h 127.0.0.1 -U postgres -d postgres 2>&1"
	if err := restoreFromHost(unit, restoreCmd, pgEnv, dump, dumpRestoreTimeout); err != nil {
		return fmt.Errorf("restore: %w. Dump preserved at %s; old data dir at %s", err, dump, backup)
	}

	if err := recordMigrateBackup(name, backup); err != nil {
		return fmt.Errorf("recording migrate backup: %w. Old data dir kept at %s", err, backup)
	}
	emit(PhaseEvent{Phase: "done", Image: targetImage, Unit: unit, Message: "Migrated. Old data dir kept at " + backup + "; remove when verified."})
	return nil
}
