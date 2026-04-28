package serviceops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// migratorFn drives a one-shot version migration for a single service family.
// It receives the lerd service name, the target image to migrate to, and an
// emit channel for streaming phase events. Implementations are expected to:
//   - Dump current data while the running container is still on the old image.
//   - Move the on-disk data dir aside (so the new container sees a fresh dir).
//   - Persist the new image into the user config and regenerate the quadlet.
//   - Restart the unit on the new image and wait for it ready.
//   - Restore the dump into the new container.
//
// Backups land in config.BackupsDir() so manual recovery is always possible.
type migratorFn func(name, targetImage string, emit func(PhaseEvent)) error

// migrators is restricted to families with stable text-format dumps that load
// cleanly across versions (SQL via mysqldump / pg_dumpall). Engines whose
// dumps are version-specific binary blobs need manual upgrades.
var migrators = map[string]migratorFn{
	"mysql":    migrateMysql,
	"mariadb":  migrateMysql,
	"postgres": migratePostgres,
}

// SupportsMigration reports whether a registered family migrator exists for
// the named service. Used by the UI to decide whether to render the Migrate
// button alongside (or instead of) Upgrade.
func SupportsMigration(name string) bool {
	_, ok := migrators[familyOf(name)]
	return ok
}

// MigrateService runs a per-family dump/restore migration so the service can
// move across data-incompatible SQL versions (e.g. mysql 8.0 → 9.0, postgres
// 16 → 17). Errors when no handler is registered for the service family.
func MigrateService(name, targetImage string, emit func(PhaseEvent)) error {
	fam := familyOf(name)
	fn, ok := migrators[fam]
	if !ok {
		return fmt.Errorf("no migration handler for %s (family=%q) — dump and restore manually following the service's docs", name, fam)
	}
	if targetImage == "" {
		return fmt.Errorf("targetImage is required")
	}
	if err := os.MkdirAll(config.BackupsDir(), 0755); err != nil {
		return fmt.Errorf("creating backups dir: %w", err)
	}
	return fn(name, targetImage, emit)
}

// familyOf returns the service family for a default preset or installed custom
// service. Used to dispatch to the right migrator handler.
func familyOf(name string) string {
	if config.IsDefaultPreset(name) {
		if p, err := config.LoadPreset(name); err == nil {
			return p.Family
		}
	}
	if svc, err := config.LoadCustomService(name); err == nil {
		if svc.Family != "" {
			return svc.Family
		}
		return config.InferFamily(svc.Name)
	}
	return ""
}

// timestamped returns a UTC ISO-ish timestamp safe for filenames.
func timestamped() string { return time.Now().UTC().Format("20060102-150405") }

// containerExec runs a command inside a running container with stdin piped
// from r and stdout/stderr captured. Used by every migrator's dump+restore
// steps so we don't ship per-family copies of this plumbing.
func containerExec(container, shellCmd string, stdin *os.File) ([]byte, error) {
	cmd := exec.Command(podman.PodmanBin(), "exec", "-i", container, "sh", "-c", shellCmd)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	return cmd.CombinedOutput()
}

// dumpToHost streams the output of a container command into a host file path.
func dumpToHost(container, shellCmd, hostPath string) error {
	out, err := os.Create(hostPath)
	if err != nil {
		return fmt.Errorf("creating dump file %s: %w", hostPath, err)
	}
	defer out.Close()
	cmd := exec.Command(podman.PodmanBin(), "exec", container, "sh", "-c", shellCmd)
	cmd.Stdout = out
	stderr, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("dump command failed: %w\n%s", err, string(stderr))
	}
	return nil
}

// restoreFromHost streams a host file into a container command's stdin.
func restoreFromHost(container, shellCmd, hostPath string) error {
	in, err := os.Open(hostPath)
	if err != nil {
		return fmt.Errorf("opening dump file %s: %w", hostPath, err)
	}
	defer in.Close()
	cmd := exec.Command(podman.PodmanBin(), "exec", "-i", container, "sh", "-c", shellCmd)
	cmd.Stdin = in
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restore command failed: %w\n%s", err, string(out))
	}
	return nil
}

// swapDataDirAside moves the current data dir to a timestamped backup name so
// the new container starts from an empty dir. Returns the backup path so the
// caller can restore it on failure.
func swapDataDirAside(svcName string) (string, error) {
	src := config.DataSubDir(svcName)
	if _, err := os.Stat(src); err != nil {
		return "", nil // no data to swap — fresh container or volume-managed
	}
	dst := src + ".pre-migrate-" + timestamped()
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("moving data dir aside: %w", err)
	}
	if err := os.MkdirAll(src, 0755); err != nil {
		// Best-effort restore: put the original back if we can't make a new one.
		_ = os.Rename(dst, src)
		return "", fmt.Errorf("creating fresh data dir: %w", err)
	}
	return dst, nil
}

// restoreDataDirFromBackup undoes swapDataDirAside, used when migration fails
// before the new image is committed.
func restoreDataDirFromBackup(svcName, backupPath string) error {
	if backupPath == "" {
		return nil
	}
	src := config.DataSubDir(svcName)
	_ = os.RemoveAll(src)
	return os.Rename(backupPath, src)
}

// switchToTargetImage persists the new image, regenerates the quadlet, and
// restarts the unit. Reuses the same path the streaming Update flow uses so
// behavior is consistent across update/upgrade/migrate.
func switchToTargetImage(name, targetImage string, emit func(PhaseEvent)) error {
	emit(PhaseEvent{Phase: "writing_quadlet", Image: targetImage})
	if err := persistImageChoice(name, targetImage); err != nil {
		return err
	}
	unit := "lerd-" + name
	emit(PhaseEvent{Phase: "restarting_unit", Unit: unit})
	for attempt := range 5 {
		if err := podman.RestartUnit(unit); err == nil {
			return nil
		} else if !strings.Contains(err.Error(), "not found") {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	return fmt.Errorf("restarting %s timed out", unit)
}

// waitContainerReady polls a service-specific readiness probe by exec'ing a
// trivial command inside the container until it succeeds or timeout elapses.
func waitContainerReady(container, probeCmd string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := containerExec(container, probeCmd, nil); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("container %s never became ready within %s", container, timeout)
}

// ---- mysql / mariadb ---------------------------------------------------------

func migrateMysql(name, targetImage string, emit func(PhaseEvent)) error {
	unit := "lerd-" + name
	dump := filepath.Join(config.BackupsDir(), name+"-"+timestamped()+".sql")

	emit(PhaseEvent{Phase: "dumping_data", Message: "mysqldump → " + dump})
	dumpCmd := "mysqldump -uroot -plerd --all-databases --single-transaction --routines --triggers --events --quick"
	if err := dumpToHost(unit, dumpCmd, dump); err != nil {
		return fmt.Errorf("mysqldump: %w", err)
	}

	emit(PhaseEvent{Phase: "stopping_unit", Unit: unit})
	if err := podman.StopUnit(unit); err != nil {
		return fmt.Errorf("stopping %s: %w", unit, err)
	}

	emit(PhaseEvent{Phase: "swapping_data_dir", Message: "old data preserved alongside the dump"})
	backup, err := swapDataDirAside(name)
	if err != nil {
		_ = podman.StartUnit(unit)
		return err
	}

	emit(PhaseEvent{Phase: "pulling_image", Image: targetImage})
	if err := podman.PullImageWithProgress(targetImage, func(line string) {
		emit(PhaseEvent{Phase: "pulling_image", Message: line})
	}); err != nil {
		_ = restoreDataDirFromBackup(name, backup)
		_ = podman.StartUnit(unit)
		return fmt.Errorf("pulling target: %w", err)
	}

	if err := switchToTargetImage(name, targetImage, emit); err != nil {
		_ = restoreDataDirFromBackup(name, backup)
		return err
	}

	emit(PhaseEvent{Phase: "waiting_ready", Unit: unit})
	probe := "mysql -uroot -plerd -e 'SELECT 1' >/dev/null 2>&1 || mariadb -uroot -plerd -e 'SELECT 1' >/dev/null 2>&1"
	if err := waitContainerReady(unit, probe, 90*time.Second); err != nil {
		return fmt.Errorf("%w. Dump preserved at %s", err, dump)
	}

	emit(PhaseEvent{Phase: "restoring_data", Message: dump})
	restoreCmd := "mysql -uroot -plerd 2>&1 || mariadb -uroot -plerd 2>&1"
	if err := restoreFromHost(unit, restoreCmd, dump); err != nil {
		return fmt.Errorf("restore: %w. Dump preserved at %s; old data dir at %s", err, dump, backup)
	}

	emit(PhaseEvent{Phase: "done", Image: targetImage, Unit: unit, Message: "Migrated. Old data dir kept at " + backup + "; remove when verified."})
	return nil
}

// ---- postgres ----------------------------------------------------------------

func migratePostgres(name, targetImage string, emit func(PhaseEvent)) error {
	unit := "lerd-" + name
	dump := filepath.Join(config.BackupsDir(), name+"-"+timestamped()+".sql")

	emit(PhaseEvent{Phase: "dumping_data", Message: "pg_dumpall → " + dump})
	dumpCmd := "PGPASSWORD=lerd pg_dumpall -h 127.0.0.1 -U postgres --clean --if-exists"
	if err := dumpToHost(unit, dumpCmd, dump); err != nil {
		return fmt.Errorf("pg_dumpall: %w", err)
	}

	emit(PhaseEvent{Phase: "stopping_unit", Unit: unit})
	if err := podman.StopUnit(unit); err != nil {
		return fmt.Errorf("stopping %s: %w", unit, err)
	}

	emit(PhaseEvent{Phase: "swapping_data_dir"})
	backup, err := swapDataDirAside(name)
	if err != nil {
		_ = podman.StartUnit(unit)
		return err
	}

	emit(PhaseEvent{Phase: "pulling_image", Image: targetImage})
	if err := podman.PullImageWithProgress(targetImage, func(line string) {
		emit(PhaseEvent{Phase: "pulling_image", Message: line})
	}); err != nil {
		_ = restoreDataDirFromBackup(name, backup)
		_ = podman.StartUnit(unit)
		return fmt.Errorf("pulling target: %w", err)
	}

	if err := switchToTargetImage(name, targetImage, emit); err != nil {
		_ = restoreDataDirFromBackup(name, backup)
		return err
	}

	emit(PhaseEvent{Phase: "waiting_ready", Unit: unit})
	probe := "PGPASSWORD=lerd psql -h 127.0.0.1 -U postgres -c 'SELECT 1' >/dev/null 2>&1"
	if err := waitContainerReady(unit, probe, 90*time.Second); err != nil {
		return fmt.Errorf("%w. Dump preserved at %s", err, dump)
	}

	emit(PhaseEvent{Phase: "restoring_data", Message: dump})
	restoreCmd := "PGPASSWORD=lerd psql -h 127.0.0.1 -U postgres -d postgres 2>&1"
	if err := restoreFromHost(unit, restoreCmd, dump); err != nil {
		return fmt.Errorf("restore: %w. Dump preserved at %s; old data dir at %s", err, dump, backup)
	}

	emit(PhaseEvent{Phase: "done", Image: targetImage, Unit: unit, Message: "Migrated. Old data dir kept at " + backup + "; remove when verified."})
	return nil
}
