package serviceops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// Sentinel errors returned by SaveTuningOverride so callers (CLI, HTTP
// handler, future MCP surface) map them to a consistent error surface
// without each one re-deriving the install/family checks. Wrapped via
// %w so callers stay free to add context (e.g. the name) before
// returning.
var (
	// ErrTuningServiceNotInstalled means the service has no quadlet on
	// disk. Surfaces as 404 in HTTP. Lets default-preset names that
	// resolve through LoadPreset still error cleanly when the user has
	// explicitly `lerd service remove`d them, so an edit cannot silently
	// reinstall via the regen+restart path below.
	ErrTuningServiceNotInstalled = errors.New("service is not installed")
	// ErrTuningFamilyUnsupported means the service has no tuningMounts
	// entry for its family. Surfaces as 400 in HTTP.
	ErrTuningFamilyUnsupported = errors.New("service does not support tuning")
)

// tuningServiceLocks holds a sync.Mutex per service name so concurrent
// save / reset / restore requests against DIFFERENT services run in
// parallel while operations on the SAME service still serialize. The
// previous package-wide lock blocked every tuning request across every
// service for up to ~40s when one save tripped the auto-rollback path
// (RestartUnit + WaitReady on the bad config, then another
// RestartUnit + WaitReady on the rolled-back bytes), which surfaced as
// unrelated services' modals appearing wedged on "Saving…". Per-name
// locking preserves the same in-rollback safety with no cross-service
// blocking.
var (
	tuningServiceLocksMu sync.Mutex
	tuningServiceLocks   = map[string]*sync.Mutex{}
)

func lockTuningService(name string) *sync.Mutex {
	tuningServiceLocksMu.Lock()
	mu, ok := tuningServiceLocks[name]
	if !ok {
		mu = &sync.Mutex{}
		tuningServiceLocks[name] = mu
	}
	tuningServiceLocksMu.Unlock()
	mu.Lock()
	return mu
}

// tuningRestartTimeout caps how long we wait for the service to come
// ready after a restart before deciding the new config broke it and
// auto-rolling back. Long enough for a cold-start mysql with a large
// innodb buffer pool resize; short enough that a true-broken config
// surfaces within the modal-busy window. Mutable so tests can drop it.
var tuningRestartTimeout = 20 * time.Second

// tuningSnapshotMaxBytes bounds the in-memory snapshot we capture
// before a save / restore / reset. The HTTP handler already caps the
// POST body at 64 KiB; we mirror the same ceiling here so an out-of-
// band edit that grew the on-disk file to multi-gigabytes can't OOM
// the lerd-ui process inside readTuningSnapshot. Mirrors the limit
// the env / nginx editors use on their own snapshot reads.
const tuningSnapshotMaxBytes = 64 << 10

// tuningBackupRegexCache memoises the per-service backup-name regex.
// regexp.MustCompile is cheap but not free, and the list/restore
// endpoints recompile on every request — under a tab that polls
// backups after every save the cost adds up. Compile once per service
// name and reuse. QuoteMeta neutralises regex metacharacters so the
// build cannot panic on user-supplied names.
var (
	tuningBackupRegexCacheMu sync.Mutex
	tuningBackupRegexCache   = map[string]*regexp.Regexp{}
)

// tuningBackupRegexFor returns the per-service backup-name regex used
// by every validation path. Fully anchored at both ends and the service
// name is escaped into the pattern so cross-service / traversal names
// are rejected by the regex alone, without relying on a separate prefix
// check.
func tuningBackupRegexFor(name string) *regexp.Regexp {
	tuningBackupRegexCacheMu.Lock()
	defer tuningBackupRegexCacheMu.Unlock()
	if re, ok := tuningBackupRegexCache[name]; ok {
		return re
	}
	re := regexp.MustCompile(`\A` + regexp.QuoteMeta(name) + `\.conf\.bkp\.\d{8}-\d{6}(-\d+)?\z`)
	tuningBackupRegexCache[name] = re
	return re
}

// tuningSnapshot captures the pre-write state of a tuning file so a
// failed restart (the de-facto validation step for service configs) can
// roll the bytes back to exactly what was running before.
type tuningSnapshot struct {
	existed bool
	data    []byte
	mode    os.FileMode
}

// readTuningSnapshot opens the override once and captures contents +
// mode in a single shot, closing the TOCTOU window a separate Stat +
// ReadFile pair would leave open. A missing file returns existed=false
// with a default mode so a first save can install one cleanly.
func readTuningSnapshot(name string) (tuningSnapshot, error) {
	path := config.ServiceTuningFile(name)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tuningSnapshot{mode: 0o644}, nil
		}
		return tuningSnapshot{}, fmt.Errorf("opening tuning file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return tuningSnapshot{}, fmt.Errorf("stat tuning file: %w", err)
	}
	// LimitReader matches the POST-body cap so an out-of-band write
	// that grew the file to multi-GB cannot OOM the process while we
	// hold a service lock. We read +1 byte over the cap so the size
	// check below can detect a genuine oversize file rather than
	// silently truncating one that happened to fit exactly.
	data, err := io.ReadAll(io.LimitReader(f, tuningSnapshotMaxBytes+1))
	if err != nil {
		return tuningSnapshot{}, fmt.Errorf("reading tuning file: %w", err)
	}
	if len(data) > tuningSnapshotMaxBytes {
		return tuningSnapshot{}, fmt.Errorf("tuning file exceeds %d bytes; refusing to snapshot — edit the file out-of-band to shrink it", tuningSnapshotMaxBytes)
	}
	return tuningSnapshot{existed: true, data: data, mode: info.Mode().Perm()}, nil
}

// restoreTuningSnapshot puts the override back to the state captured
// by snap. If the file did not exist before the write, this removes
// whatever the write just placed there; otherwise the prior bytes are
// written atomically with the prior mode.
func restoreTuningSnapshot(name string, snap tuningSnapshot) error {
	path := config.ServiceTuningFile(name)
	if !snap.existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rolling back new tuning file: %w", err)
		}
		return nil
	}
	if err := writeTuningFileAtomic(path, snap.data, snap.mode); err != nil {
		return fmt.Errorf("rolling back tuning contents: %w", err)
	}
	return nil
}

// writeTuningFileAtomic stages data into a temp file inside the backup
// directory (a sibling of service-tuning/) and renames the result into
// place. Putting the temp outside the live tuning dir keeps a
// half-written file invisible to anything that might scan service-
// tuning/ for *.conf, and same-filesystem renames remain atomic because
// both directories live under DataDir().
func writeTuningFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating tuning dir: %w", err)
	}
	stageDir := config.ServiceTuningBkpDir()
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("creating stage dir: %w", err)
	}
	tmp, err := os.CreateTemp(stageDir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming into place: %w", err)
	}
	return nil
}

// uniqueTuningBackupPath returns a backup path inside the shared backup
// dir that does not collide with an existing file. Two saves in the
// same wall-clock second would otherwise overwrite the earlier backup;
// this appends a -<n> suffix until it finds a free name and returns an
// error on exhaustion instead of silently overwriting.
func uniqueTuningBackupPath(name string, now time.Time) (string, error) {
	dir := config.ServiceTuningBkpDir()
	base := filepath.Join(dir, name+".conf.bkp."+now.Format("20060102-150405"))
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base, nil
	}
	for i := 1; i < 1_000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("backup name space exhausted for %s", name)
}

// writeTuningBackup stages snap.data into the backup directory with a
// unique timestamped name. Mode is preserved from the original file.
// Returns the absolute backup path and the base name for the API
// response. A no-op when snap.existed is false (first save, nothing
// to back up).
func writeTuningBackup(name string, snap tuningSnapshot, now time.Time) (string, string, error) {
	if !snap.existed {
		return "", "", nil
	}
	dir := config.ServiceTuningBkpDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("creating backup dir: %w", err)
	}
	backupPath, err := uniqueTuningBackupPath(name, now)
	if err != nil {
		return "", "", err
	}
	if err := writeTuningFileAtomic(backupPath, snap.data, snap.mode); err != nil {
		return "", "", fmt.Errorf("writing backup: %w", err)
	}
	return backupPath, filepath.Base(backupPath), nil
}

// TuningBackup is one row of the GET /api/services/{name}/config/backups
// response. Mirrors SiteNginxBackup so the frontend reuses the same
// list / diff modal / restore plumbing.
type TuningBackup struct {
	Name      string `json:"name"`
	MtimeUnix int64  `json:"mtime_unix"`
}

// ListTuningBackups enumerates timestamped backups for a service,
// newest first. Returns an empty slice (not nil) when the directory
// exists but holds no matching files, and nil when the directory does
// not exist; an unreachable directory returns an error so the UI can
// distinguish a truly-empty backup set from a read failure.
func ListTuningBackups(name string) ([]TuningBackup, error) {
	dir := config.ServiceTuningBkpDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	re := tuningBackupRegexFor(name)
	out := []TuningBackup{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		nm := e.Name()
		if !re.MatchString(nm) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, TuningBackup{Name: nm, MtimeUnix: info.ModTime().Unix()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// RestoreTuningBackup copies the named backup over the live tuning
// file atomically and returns the restored content plus the backup
// path (the caller deletes the backup only after a successful
// restart, so a failed restart leaves the recovery copy on disk).
//
// IMPORTANT: callers must hold the per-service tuning lock via
// lockTuningService(name). External callers should use
// RestoreTuningFromBackup which acquires the lock and drives the full
// restart + auto-rollback pipeline; the lower-level helper exists
// because RestoreTuningFromBackup composes it.
func RestoreTuningBackup(name, backupName string) (string, string, error) {
	if !tuningBackupRegexFor(name).MatchString(backupName) {
		return "", "", fmt.Errorf("invalid backup name")
	}
	backupPath := filepath.Join(config.ServiceTuningBkpDir(), backupName)
	backupInfo, statErr := os.Stat(backupPath)
	if statErr != nil {
		return "", "", fmt.Errorf("stat backup: %w", statErr)
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return "", "", fmt.Errorf("reading backup: %w", err)
	}
	livePath := config.ServiceTuningFile(name)
	mode := backupInfo.Mode().Perm()
	if info, err := os.Stat(livePath); err == nil {
		mode = info.Mode().Perm()
	}
	if err := writeTuningFileAtomic(livePath, data, mode); err != nil {
		return "", "", err
	}
	return string(data), backupPath, nil
}

// TuningSaveResult carries the information the HTTP handler needs to
// build a response. Content/Exists mirror what's on disk after the
// operation finished (whether or not the restart succeeded) so the
// client can refresh its `original` baseline even on the reload-
// failure path.
type TuningSaveResult struct {
	BackupName    string
	RolledBack    bool
	ContentOnDisk string
	Exists        bool
}

// readTuningContent returns the bytes currently on disk plus whether
// the file is present. Used to populate the response after a save /
// rollback so the client can refresh its baseline without an extra GET.
func readTuningContent(name string) (string, bool) {
	data, err := os.ReadFile(config.ServiceTuningFile(name))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// SaveTuningOverride is the single entry point for writing the user
// tuning override file, regenerating the quadlet so the override
// Volume= mount is present on installs predating the feature, and
// restarting the unit so it re-reads the config. Shared by the
// `lerd service config` CLI command and the
// `/api/services/{name}/config` HTTP handler; matches the pattern of
// xdebugops.Apply.
//
// When backup is true and a previous version of the file exists, a
// timestamped copy lands in ServiceTuningBkpDir() BEFORE the new bytes
// land so a crash between the backup write and the live write still
// leaves a recoverable copy on disk. An in-memory snapshot is taken
// regardless of `backup` so a failed restart auto-rolls the live file
// back to the pre-save bytes and re-restarts the service; the user
// only loses their unsaved edits, not the running service.
//
// Order:
//  1. ServiceInstalled guard — block silent-reinstall-on-edit for
//     removed default presets that ResolveServiceForTuning would
//     otherwise still resolve via LoadPreset.
//  2. ResolveServiceForTuning + ServiceTuningMount — fail fast with
//     family-unsupported for services that don't expose a mount.
//  3. MaterializeServiceTuning — seed the template on first save.
//  4. Snapshot current bytes for rollback.
//  5. Stage a backup file when requested.
//  6. Write `content` atomically.
//  7. EnsureTuningQuadlet — regen so a freshly-written file isn't
//     orphaned on installs predating the feature.
//  8. Restart the unit so the container re-reads the override.
//  9. WaitReady — if the new config crashes the service, restoreTuning
//     Snapshot + restart again and report the failure.
func SaveTuningOverride(name, content string, backup bool) (TuningSaveResult, error) {
	res := TuningSaveResult{}
	if !ServiceInstalled(name) {
		return res, fmt.Errorf("%w: run `lerd service preset install %s` first", ErrTuningServiceNotInstalled, name)
	}
	svc, err := config.ResolveServiceForTuning(name)
	if err != nil {
		return res, fmt.Errorf("%w: %s", ErrTuningServiceNotInstalled, err.Error())
	}
	if _, ok := config.ServiceTuningMount(svc); !ok {
		return res, fmt.Errorf("%w (family %q)", ErrTuningFamilyUnsupported, config.FamilyOf(svc))
	}
	if err := config.MaterializeServiceTuning(svc); err != nil {
		return res, fmt.Errorf("creating tuning file: %w", err)
	}
	template, _ := config.ServiceTuningTemplate(svc)

	unlock := lockTuningService(name)
	defer unlock.Unlock()

	snap, err := readTuningSnapshot(name)
	if err != nil {
		return res, err
	}
	backupPath := ""
	// Skip the staged backup when the file is still just the seeded
	// template: backing it up would clutter the restore list with a
	// copy that anyone can re-materialise on demand. The frontend
	// already hides the checkbox via cfg.exists, but the guard here
	// covers CLI / MCP / future callers that pass backup=true on a
	// freshly-materialized service.
	if backup && snap.existed && string(snap.data) != template {
		bp, bn, err := writeTuningBackup(name, snap, time.Now())
		if err != nil {
			return res, err
		}
		backupPath = bp
		res.BackupName = bn
	}
	livePath := config.ServiceTuningFile(name)
	if err := writeTuningFileAtomic(livePath, []byte(content), snap.mode); err != nil {
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		// Surface the actual on-disk state regardless of which branch
		// returns, so the client never refreshes against an empty
		// baseline that doesn't match reality.
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, fmt.Errorf("writing tuning file: %w", err)
	}
	if err := EnsureTuningQuadlet(name, svc); err != nil {
		// Quadlet regen failed; roll back the file so the (still-
		// running) service doesn't pick up the new config the next
		// time something restarts it out-of-band. Re-run the regen
		// against the restored snapshot so any partial .container
		// rewrite from this attempt is overwritten with the prior
		// (consistent) shape.
		_ = restoreTuningSnapshot(name, snap)
		_ = EnsureTuningQuadlet(name, svc)
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, err
	}
	if err := podman.RestartUnit("lerd-" + name); err != nil {
		res.RolledBack = tryRollback(name, snap, backupPath)
		if res.RolledBack {
			res.BackupName = ""
		}
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, restartErr(name, err, res.RolledBack)
	}
	if err := podman.WaitReady(name, tuningRestartTimeout); err != nil {
		res.RolledBack = tryRollback(name, snap, backupPath)
		if res.RolledBack {
			res.BackupName = ""
		}
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, readyErr(name, err, res.RolledBack)
	}
	res.ContentOnDisk, res.Exists = readTuningContent(name)
	return res, nil
}

// restartErr formats the failure message for the auto-rollback path.
// When the rollback succeeded the wording switches to "save reverted"
// so the user sees a recovery-aware message instead of a bare
// "restarting lerd-mysql: …" that reads as if the service is still
// broken. Callers always pair this with RolledBack in the response
// envelope.
func restartErr(name string, err error, rolledBack bool) error {
	if rolledBack {
		return fmt.Errorf("save reverted: lerd-%s would not restart with the new config, previous bytes restored: %w", name, err)
	}
	return fmt.Errorf("restarting %s: %w", "lerd-"+name, err)
}

func readyErr(name string, err error, rolledBack bool) error {
	if rolledBack {
		return fmt.Errorf("save reverted: %s did not become ready with the new config, previous bytes restored: %w", name, err)
	}
	return fmt.Errorf("service %s did not become ready: %w", name, err)
}

// tryRollback restores the on-disk file to the snapshot bytes and
// re-restarts the service. Returns true when the live service ended
// up running the prior config (the rollback succeeded), false when
// something went wrong along the way and the service may still be in
// a broken state. The staged backup is removed when the user opted
// for one — the save the backup was meant to protect never landed, so
// keeping it would be misleading on the next restore.
func tryRollback(name string, snap tuningSnapshot, backupPath string) bool {
	if err := restoreTuningSnapshot(name, snap); err != nil {
		return false
	}
	if backupPath != "" {
		_ = os.Remove(backupPath)
	}
	if err := podman.RestartUnit("lerd-" + name); err != nil {
		return false
	}
	if err := podman.WaitReady(name, tuningRestartTimeout); err != nil {
		return false
	}
	return true
}

// ResetTuningOverride replaces the user-editable tuning file with the
// bundled commented template (no active directives) and restarts the
// service so it picks up the bundled defaults. The file is NOT removed
// because the generated quadlet declares a Volume= bind mount at the
// same path; a missing source path would make podman refuse to start
// the container. Overwriting with the template keeps the mount valid
// AND lets the user see the same "what could I tune" hints they see on
// first save. Backups are preserved in ServiceTuningBkpDir() so a
// Restore can recover from an accidental reset.
// TuningResetResult mirrors TuningSaveResult so the HTTP handler can
// surface the same recovery state to the modal. RolledBack is true
// when the template restart failed but the prior bytes were
// successfully restored and the service is back on its previous
// config. AutoBackupName is set when the reset staged an implicit
// backup of the pre-reset content (so the user can recover even if
// they never ticked the explicit backup checkbox on prior saves).
type TuningResetResult struct {
	RolledBack     bool
	AutoBackupName string
	ContentOnDisk  string
	Exists         bool
}

func ResetTuningOverride(name string) (TuningResetResult, error) {
	res := TuningResetResult{}
	if !ServiceInstalled(name) {
		return res, fmt.Errorf("%w: run `lerd service preset install %s` first", ErrTuningServiceNotInstalled, name)
	}
	svc, err := config.ResolveServiceForTuning(name)
	if err != nil {
		return res, fmt.Errorf("%w: %s", ErrTuningServiceNotInstalled, err.Error())
	}
	if _, ok := config.ServiceTuningMount(svc); !ok {
		return res, fmt.Errorf("%w (family %q)", ErrTuningFamilyUnsupported, config.FamilyOf(svc))
	}
	template, ok := config.ServiceTuningTemplate(svc)
	if !ok {
		return res, fmt.Errorf("%w (family %q)", ErrTuningFamilyUnsupported, config.FamilyOf(svc))
	}

	unlock := lockTuningService(name)
	defer unlock.Unlock()

	path := config.ServiceTuningFile(name)
	snap, err := readTuningSnapshot(name)
	if err != nil {
		return res, err
	}
	if snap.existed && string(snap.data) == template {
		// File is already the seeded template; reset is a no-op so
		// we skip the restart and avoid a wasted podman exec.
		res.ContentOnDisk = template
		res.Exists = true
		return res, nil
	}
	// Always stage an implicit backup of the pre-reset content (when
	// any exists). The UI copy promises "backups are kept and can be
	// restored later", which only held if the user had explicitly
	// ticked the backup checkbox on a previous save. Reset itself is
	// a destructive overwrite, so taking a recovery snapshot here
	// makes the promise unconditional.
	if snap.existed && string(snap.data) != template {
		_, bn, bErr := writeTuningBackup(name, snap, time.Now())
		if bErr == nil {
			res.AutoBackupName = bn
		}
	}
	if err := writeTuningFileAtomic(path, []byte(template), snap.mode); err != nil {
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, fmt.Errorf("writing template: %w", err)
	}
	// Ensure the Volume= bind mount is present in the quadlet on
	// installs predating the feature — without this, the running
	// container has no mount for ServiceTuningFile and the template
	// we just wrote is invisible on the next restart. Save and
	// Restore already do this; Reset has to as well.
	if err := EnsureTuningQuadlet(name, svc); err != nil {
		_ = restoreTuningSnapshot(name, snap)
		_ = EnsureTuningQuadlet(name, svc)
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, err
	}
	if !snap.existed {
		// File was missing before reset (someone deleted it out-of-
		// band, or the user is on a fresh install where the editor
		// has never been opened). The running service is already on
		// bundled defaults, so materialising the template is enough
		// to make the Volume= bind mount valid for the NEXT restart
		// without needing one now.
		res.ContentOnDisk = template
		res.Exists = true
		return res, nil
	}
	if err := podman.RestartUnit("lerd-" + name); err != nil {
		res.RolledBack = tryRollback(name, snap, "")
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, restartErr(name, err, res.RolledBack)
	}
	if err := podman.WaitReady(name, tuningRestartTimeout); err != nil {
		res.RolledBack = tryRollback(name, snap, "")
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, readyErr(name, err, res.RolledBack)
	}
	res.ContentOnDisk, res.Exists = readTuningContent(name)
	return res, nil
}

// TuningRestoreResult carries the post-restore state, including the
// recovery flag that lets the HTTP layer tell a successful restore
// from one that needed an auto-rollback to keep the service running.
type TuningRestoreResult struct {
	Content       string
	RolledBack    bool
	ContentOnDisk string
	Exists        bool
}

// RestoreTuningFromBackup is the entry point for the HTTP restore
// endpoint. Locks the per-service mutex, restores the named backup
// over the live file, restarts the service, and only removes the
// backup when the restart succeeded. If the restored bytes themselves
// crash the service (e.g. a stale backup with directives the current
// image rejects), the prior bytes are restored and the service is
// re-restarted so the user only loses the attempted restore, not the
// running service.
func RestoreTuningFromBackup(name, backupName string) (TuningRestoreResult, error) {
	res := TuningRestoreResult{}
	if !ServiceInstalled(name) {
		return res, fmt.Errorf("%w: run `lerd service preset install %s` first", ErrTuningServiceNotInstalled, name)
	}
	svc, err := config.ResolveServiceForTuning(name)
	if err != nil {
		return res, fmt.Errorf("%w: %s", ErrTuningServiceNotInstalled, err.Error())
	}
	if _, ok := config.ServiceTuningMount(svc); !ok {
		return res, fmt.Errorf("%w (family %q)", ErrTuningFamilyUnsupported, config.FamilyOf(svc))
	}

	unlock := lockTuningService(name)
	defer unlock.Unlock()

	// Snapshot the pre-restore state BEFORE writing the backup over
	// the live file, so a failed restart can roll the bytes back to
	// what was actually working a moment ago. The backup we are
	// restoring is preserved on disk through this whole flow; we
	// only remove it after a successful restart.
	snap, err := readTuningSnapshot(name)
	if err != nil {
		return res, err
	}
	content, backupPath, err := RestoreTuningBackup(name, backupName)
	if err != nil {
		return res, err
	}
	res.Content = content
	if err := EnsureTuningQuadlet(name, svc); err != nil {
		_ = restoreTuningSnapshot(name, snap)
		_ = EnsureTuningQuadlet(name, svc)
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, err
	}
	if err := podman.RestartUnit("lerd-" + name); err != nil {
		res.RolledBack = tryRollback(name, snap, "")
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, restartErr(name, err, res.RolledBack)
	}
	if err := podman.WaitReady(name, tuningRestartTimeout); err != nil {
		res.RolledBack = tryRollback(name, snap, "")
		res.ContentOnDisk, res.Exists = readTuningContent(name)
		return res, readyErr(name, err, res.RolledBack)
	}
	_ = os.Remove(backupPath)
	res.ContentOnDisk, res.Exists = readTuningContent(name)
	return res, nil
}

// ReadTuningBackupContent returns the raw bytes of a named backup so
// the restore modal can show a diff before the user accepts. Validates
// the backup name against the per-service anchored regex, then reads
// the file directly — the caller is the HTTP handler so the response
// streams from this byte slice.
func ReadTuningBackupContent(name, backupName string) ([]byte, error) {
	if !tuningBackupRegexFor(name).MatchString(backupName) {
		return nil, fmt.Errorf("invalid backup name")
	}
	data, err := os.ReadFile(filepath.Join(config.ServiceTuningBkpDir(), backupName))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// EnsureTuningQuadlet rewrites the quadlet for `name` so the tuning
// override Volume= mount is present on installs predating the feature.
// Built-in default presets regenerate through EnsureDefaultPresetQuadlet
// (which itself resolves to EnsureCustomServiceQuadlet, so the mount
// lands either way); custom-YAML services regenerate through
// EnsureCustomServiceQuadlet directly.
//
// Split out from SaveTuningOverride so the CLI's `lerd service config`
// (whose editor writes to the override file out-of-band, and which has
// a `--no-restart` flag) can share the regen step without forcing a
// restart. Failures are propagated, NOT logged-and-ignored — skipping
// the regen orphans the user's just-written override and the next
// restart would re-read the OLD config (no mount, no values picked up).
func EnsureTuningQuadlet(name string, svc *config.CustomService) error {
	if config.IsDefaultPreset(name) {
		if err := EnsureDefaultPresetQuadlet(name); err != nil {
			return fmt.Errorf("regenerating quadlet for %s: %w", name, err)
		}
		return nil
	}
	if err := EnsureCustomServiceQuadlet(svc); err != nil {
		return fmt.Errorf("regenerating quadlet for %s: %w", name, err)
	}
	return nil
}
