package serviceops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// RemoveOptions controls optional side effects of RemoveService.
type RemoveOptions struct {
	// RemoveData renames the service's data directory to a timestamped
	// `.pre-remove-<ts>` sibling. The data is recoverable by renaming back.
	// On EXDEV the helper falls back to copy-tree + delete so the
	// recoverability promise holds across filesystems too.
	RemoveData bool

	// SkipFamilyRegen suppresses the post-remove RegenerateFamilyConsumers
	// pass. Set by ReinstallService, which then drives the regen itself
	// after install: InstallPresetByName regenerates internally for custom
	// services, while default-preset and failure paths call regen
	// explicitly. The regen-during-remove was racing the post-install
	// regen on macOS (launchctl bootout/bootstrap can fall through to
	// kickstart which doesn't re-read the plist), leaving consumers on
	// the partial plist.
	SkipFamilyRegen bool
}

// Internal seams so tests can swap out the real podman calls. The defaults
// route to the real package functions.
var (
	removeStopUnit     = podman.StopUnit
	removeContainerFn  = podman.RemoveContainer
	removeQuadletFn    = podman.RemoveQuadlet
	removeUnitStatusFn = podman.UnitStatus

	// removeRegenerateFamilyFn lets tests observe whether the regen pass
	// fired without spinning up a real consumer. Defaults to the real
	// RegenerateFamilyConsumers.
	removeRegenerateFamilyFn = RegenerateFamilyConsumers

	// osRenameFn is the rename seam used by renameDataAside. Tests swap it
	// to inject EXDEV without needing two filesystems.
	osRenameFn = os.Rename
)

// RemoveService stops, removes, and (optionally) wipes the data of a service.
// It is the single entry point shared by the CLI, MCP, and UI handlers.
//
// Order:
//  1. emit stopping_unit, StopUnit (only if active/activating; abort on error)
//  2. emit removing_container, RemoveContainer
//  3. (if RemoveData) emit removing_data, rename-aside ~/.local/share/lerd/data/<name>
//  4. emit removing_quadlet, RemoveQuadlet, DaemonReload
//  5. emit removing_config, RemoveCustomService (no-op for default presets)
//  6. emit regenerating_consumers, RegenerateFamilyConsumers (if family known)
//  7. emit done
//
// emit may be nil. Default-preset services are accepted: their YAML doesn't
// exist on disk, so RemoveCustomService is a tolerated no-op.
func RemoveService(name string, opts RemoveOptions, emit func(PhaseEvent)) error {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}
	unit := "lerd-" + name

	var family string
	if existing, err := config.LoadCustomService(name); err == nil {
		family = existing.Family
	}

	emit(PhaseEvent{Phase: "stopping_unit", Unit: unit})
	status, _ := removeUnitStatusFn(unit)
	if status == "active" || status == "activating" {
		if err := removeStopUnit(unit); err != nil {
			return fmt.Errorf("stop %s: %w", unit, err)
		}
	}

	emit(PhaseEvent{Phase: "removing_container", Unit: unit})
	removeContainerFn(unit)

	if opts.RemoveData {
		dir := config.DataSubDir(name)
		emit(PhaseEvent{Phase: "removing_data", Message: dir})
		if err := renameDataAside(dir); err != nil {
			return fmt.Errorf("rename data aside for %s: %w", name, err)
		}
		// Tuning overrides survive `service remove` (and reinstall) by design,
		// but an explicit data-wipe should take the override with it — the
		// user is asking for a clean slate, and a stale override silently
		// applied to the next install would be surprising. The file is just
		// a text override, not state, so no rename-aside.
		tuningPath := config.ServiceTuningFile(name)
		if err := os.Remove(tuningPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove tuning override for %s: %w", name, err)
		}
	}

	emit(PhaseEvent{Phase: "removing_quadlet", Unit: unit})
	if err := removeQuadletFn(unit); err != nil {
		return fmt.Errorf("remove quadlet %s: %w", unit, err)
	}
	_ = podman.DaemonReloadFn()

	emit(PhaseEvent{Phase: "removing_config"})
	if err := config.RemoveCustomService(name); err != nil {
		return fmt.Errorf("remove service config: %w", err)
	}

	emit(PhaseEvent{Phase: "regenerating_consumers"})
	if family != "" && !opts.SkipFamilyRegen {
		removeRegenerateFamilyFn(family)
	}

	emit(PhaseEvent{Phase: "done"})
	return nil
}

// renameDataAside renames dir to `<dir>.pre-remove-<unix-nanos>` so the data
// is recoverable. On EXDEV (the aside target lives on a different filesystem)
// it falls back to copy-tree + delete so the recoverability promise holds
// across filesystems too — silently os.RemoveAll'ing the source would destroy
// data the user expected to be recoverable. A missing source is a no-op.
func renameDataAside(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	aside := fmt.Sprintf("%s.pre-remove-%d", dir, time.Now().UnixNano())
	if err := osRenameFn(dir, aside); err != nil {
		if !isCrossDeviceErr(err) {
			return err
		}
		if copyErr := copyTree(dir, aside); copyErr != nil {
			_ = os.RemoveAll(aside)
			return fmt.Errorf("rename-aside fallback (cross-device copy) failed: %w", copyErr)
		}
		return os.RemoveAll(dir)
	}
	return nil
}

// isCrossDeviceErr unwraps a *os.LinkError and matches syscall.EXDEV. Falls
// back to substring matching only as a last resort so non-Go-wrapped errors
// (e.g. localized macOS messages bubbled through CGo paths) are still caught.
func isCrossDeviceErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EXDEV) {
		return true
	}
	var le *os.LinkError
	if errors.As(err, &le) && errors.Is(le.Err, syscall.EXDEV) {
		return true
	}
	msg := err.Error()
	for _, needle := range []string{"invalid cross-device link", "EXDEV", "cross-device"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// copyTree walks src and recreates every file/dir under dst. Symlinks are
// recreated as symlinks (target preserved). File mode bits are preserved so
// container data dirs that rely on mode 0700 stay protected after the move.
// Used only on the EXDEV fallback path; for same-filesystem moves os.Rename
// is atomic and far cheaper.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, lerr := os.Readlink(path)
			if lerr != nil {
				return lerr
			}
			if mkErr := os.MkdirAll(filepath.Dir(target), 0o755); mkErr != nil {
				return mkErr
			}
			return os.Symlink(link, target)
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm())
		default:
			return copyFile(path, target, info.Mode().Perm())
		}
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
		return mkErr
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
