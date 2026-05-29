// Package cfgedit is a small service for editing user-owned config files that
// lerd seeds but never overwrites (per-site nginx overrides, the global
// http-level nginx override, php.ini overrides). It owns the shared mechanics:
// atomic staged writes, timestamped backups, snapshot/rollback, and the
// process-wide save lock. Callers supply validation and apply (reload/restart)
// hooks; the service never imports nginx or php so it stays a leaf dependency.
package cfgedit

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Mu serializes save / reset / restore across every File. Validators like
// `nginx -t` inspect the whole on-disk config tree, so two unsynchronized
// saves to different files could otherwise race into a false rollback. The
// operations are rare and short, so one lock for the whole service is enough.
var Mu sync.Mutex

// Backup is one timestamped backup, newest-first when listed. The JSON tags
// match what the web editor's restore dropdown already consumes.
type Backup struct {
	Name      string `json:"name"`
	MtimeUnix int64  `json:"mtime_unix"`
}

// Content is the result of reading a file (or its seeded template).
type Content struct {
	Path   string
	Body   string
	Exists bool
}

// SaveResult mirrors what the editors return to the UI, including the captured
// validator output so the modal can show the exact diagnostic.
type SaveResult struct {
	OK               bool
	Error            string
	BackupName       string
	ValidationOutput string
	Content          string
	Exists           bool
}

// RestoreResult is the outcome of restoring a backup.
type RestoreResult struct {
	OK       bool
	Error    string
	Restored string
	Content  string
}

// File describes one editable, backup-protected config file. BkpDir holds the
// timestamped backups AND doubles as the write-staging dir, so it must live
// outside any nginx include glob. Backups are named "<BkpName>.bkp.<ts>".
type File struct {
	Path     string
	BkpDir   string
	BkpName  string
	Template string
}

// SaveOpts carries the per-save knobs. Validate (optional) runs after the
// staged write; when it fails and Owns reports the failure is ours (or Owns is
// nil), the write is rolled back. Apply (optional) activates the change
// (reload/restart) after a good write; an Apply failure keeps the bytes on
// disk so the client can refresh its baseline.
type SaveOpts struct {
	Backup   bool
	Validate func(path string) (string, error)
	Owns     func(output, path string) bool
	Apply    func() error
}

// Snapshot captures pre-write state so a failed validation can roll back.
type Snapshot struct {
	Existed bool
	Data    []byte
	Mode    os.FileMode
}

func (f File) backupRegex() *regexp.Regexp {
	return regexp.MustCompile(`\A` + regexp.QuoteMeta(f.BkpName) + `\.bkp\.\d{8}-\d{6}(-\d+)?\z`)
}

// ValidBackupName reports whether name is a well-formed backup for this file
// (anchored at both ends, blocking traversal and cross-file names).
func (f File) ValidBackupName(name string) bool {
	return f.backupRegex().MatchString(name)
}

// Read returns the saved file, or the seeded template (Exists=false) when no
// file is on disk yet.
func (f File) Read() (Content, error) {
	body, err := os.ReadFile(f.Path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Content{}, err
		}
		return Content{Path: f.Path, Body: f.Template, Exists: false}, nil
	}
	return Content{Path: f.Path, Body: string(body), Exists: true}, nil
}

// ListBackups returns this file's backups, newest first.
func (f File) ListBackups() ([]Backup, error) {
	entries, err := os.ReadDir(f.BkpDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	re := f.backupRegex()
	out := []Backup{}
	for _, e := range entries {
		if e.IsDir() || !re.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Backup{Name: e.Name(), MtimeUnix: info.ModTime().Unix()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// ReadBackup returns the raw bytes of one backup, validating the name first.
// Returns os.ErrNotExist when the name is malformed or the file is gone.
func (f File) ReadBackup(name string) ([]byte, error) {
	if !f.ValidBackupName(name) {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(f.BkpDir, name))
}

// Save snapshots, optionally backs up, atomically writes, validates, and
// applies — rolling back on a validation failure this file owns. It locks the
// service-wide mutex for the whole pipeline.
func (f File) Save(content string, opt SaveOpts) (SaveResult, error) {
	Mu.Lock()
	defer Mu.Unlock()

	snap, err := ReadSnapshot(f.Path)
	if err != nil {
		return SaveResult{OK: false, Error: err.Error()}, nil
	}
	backupPath, backupName := "", ""
	if opt.Backup {
		bp, bn, err := f.WriteBackup(snap, time.Now())
		if err != nil {
			return SaveResult{OK: false, Error: err.Error()}, nil
		}
		backupPath, backupName = bp, bn
	}
	if err := f.StagedWrite([]byte(content), snap.Mode); err != nil {
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		return SaveResult{OK: false, Error: err.Error()}, nil
	}
	validationOutput := ""
	if opt.Validate != nil {
		out, testErr := opt.Validate(f.Path)
		validationOutput = out
		if testErr != nil && (opt.Owns == nil || opt.Owns(out, f.Path)) {
			if backupPath != "" {
				_ = os.Remove(backupPath)
			}
			if rbErr := RestoreSnapshot(f.Path, snap); rbErr != nil {
				return SaveResult{OK: false, Error: "config invalid and rollback failed: " + rbErr.Error(), ValidationOutput: out}, nil
			}
			return SaveResult{OK: false, Error: "config invalid, rolled back to previous contents", ValidationOutput: out}, nil
		}
	}
	if opt.Apply != nil {
		if err := opt.Apply(); err != nil {
			return SaveResult{OK: false, Error: "saved, but apply failed: " + err.Error(), BackupName: backupName, ValidationOutput: validationOutput, Content: content, Exists: true}, nil
		}
	}
	return SaveResult{OK: true, BackupName: backupName, ValidationOutput: validationOutput, Content: content, Exists: true}, nil
}

// Reset deletes the live file (its include glob then expands to nothing) and
// runs apply. Backups are kept. Apply is skipped when nothing was removed.
func (f File) Reset(apply func() error) error {
	Mu.Lock()
	defer Mu.Unlock()

	removeErr := os.Remove(f.Path)
	if removeErr != nil {
		if os.IsNotExist(removeErr) {
			return nil
		}
		return removeErr
	}
	if apply != nil {
		if err := apply(); err != nil {
			return fmt.Errorf("reset, but apply failed: %w", err)
		}
	}
	return nil
}

// Restore swaps a backup over the live file, applies, and only then drops the
// backup. An empty name restores the newest. An apply failure keeps the backup
// so the user can retry.
func (f File) Restore(name string, apply func() error) (RestoreResult, error) {
	Mu.Lock()
	defer Mu.Unlock()

	list, err := f.ListBackups()
	if err != nil {
		return RestoreResult{OK: false, Error: err.Error()}, nil
	}
	if len(list) == 0 {
		return RestoreResult{OK: false, Error: "no backup available"}, nil
	}
	if name == "" {
		name = list[0].Name
	} else {
		found := false
		for _, b := range list {
			if b.Name == name {
				found = true
				break
			}
		}
		if !found {
			return RestoreResult{OK: false, Error: "backup not found: " + name}, nil
		}
	}
	content, backupPath, err := f.restoreBackup(name)
	if err != nil {
		return RestoreResult{OK: false, Error: err.Error()}, nil
	}
	if apply != nil {
		if err := apply(); err != nil {
			return RestoreResult{OK: false, Error: "restored, but apply failed: " + err.Error(), Restored: name, Content: content}, nil
		}
	}
	_ = os.Remove(backupPath)
	return RestoreResult{OK: true, Restored: name, Content: content}, nil
}

// ReadSnapshot opens path once, capturing contents + mode (closing the TOCTOU
// window a separate Stat+ReadFile pair would leave open). A missing file is a
// non-error zero snapshot with a default mode.
func ReadSnapshot(path string) (Snapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{Mode: 0o644}, nil
		}
		return Snapshot{}, fmt.Errorf("opening config file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Snapshot{}, fmt.Errorf("stat config file: %w", err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return Snapshot{}, fmt.Errorf("reading config file: %w", err)
	}
	return Snapshot{Existed: true, Data: data, Mode: info.Mode().Perm()}, nil
}

// RestoreSnapshot puts path back to snap: removing a file that did not exist
// before, or rewriting the prior bytes+mode atomically.
func RestoreSnapshot(path string, snap Snapshot) error {
	if !snap.Existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rolling back new file: %w", err)
		}
		return nil
	}
	if err := writeFileAtomic(path, snap.Data, snap.Mode); err != nil {
		return fmt.Errorf("rolling back contents: %w", err)
	}
	return nil
}

// WriteBackup stages snap.Data into BkpDir with a unique timestamped name and
// the original mode. Returns the absolute path (for rollback cleanup) and the
// base name (for the API response). A snapshot of a missing file is a no-op.
func (f File) WriteBackup(snap Snapshot, now time.Time) (string, string, error) {
	if !snap.Existed {
		return "", "", nil
	}
	if err := os.MkdirAll(f.BkpDir, 0o755); err != nil {
		return "", "", fmt.Errorf("creating backup dir: %w", err)
	}
	backupPath, err := f.uniqueBackupPath(now)
	if err != nil {
		return "", "", err
	}
	if err := writeFileAtomic(backupPath, snap.Data, snap.Mode); err != nil {
		return "", "", fmt.Errorf("writing backup: %w", err)
	}
	return backupPath, filepath.Base(backupPath), nil
}

func (f File) uniqueBackupPath(now time.Time) (string, error) {
	base := filepath.Join(f.BkpDir, f.BkpName+".bkp."+now.Format("20060102-150405"))
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base, nil
	}
	for i := 1; i < 1_000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("backup name space exhausted for %s in %s", f.BkpName, f.BkpDir)
}

// restoreBackup copies the named backup over the live file atomically and
// returns its content + path; the caller deletes the backup only after a
// successful apply. Mode prefers the existing target's perm, else the backup's.
func (f File) restoreBackup(name string) (string, string, error) {
	if !f.ValidBackupName(name) {
		return "", "", fmt.Errorf("invalid backup name")
	}
	backupPath := filepath.Join(f.BkpDir, name)
	backupInfo, statErr := os.Stat(backupPath)
	if statErr != nil {
		return "", "", fmt.Errorf("stat backup: %w", statErr)
	}
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		return "", "", fmt.Errorf("reading backup: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return "", "", fmt.Errorf("creating target dir: %w", err)
	}
	mode := backupInfo.Mode().Perm()
	if info, err := os.Stat(f.Path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := writeFileAtomic(f.Path, backupData, mode); err != nil {
		return "", "", err
	}
	return string(backupData), backupPath, nil
}

// StagedWrite writes data to Path by staging a temp file in BkpDir (off any
// include glob) and renaming it into place, so nginx never sees a half-written
// file while keeping same-filesystem rename atomicity.
func (f File) StagedWrite(data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return fmt.Errorf("creating target dir: %w", err)
	}
	if err := os.MkdirAll(f.BkpDir, 0o755); err != nil {
		return fmt.Errorf("creating stage dir: %w", err)
	}
	tmp, err := os.CreateTemp(f.BkpDir, filepath.Base(f.Path)+".tmp.*")
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
	if err := os.Rename(tmpPath, f.Path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming into place: %w", err)
	}
	return nil
}

// MentionsFile reports whether validator output names the file we just wrote.
// nginx -t walks the whole tree, so a pre-existing broken neighbour must not
// roll back our valid write; callers pass this as SaveOpts.Owns.
func MentionsFile(output, path string) bool {
	return strings.Contains(output, filepath.Base(path))
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	effective := mode
	if info, err := os.Stat(path); err == nil {
		effective = info.Mode().Perm()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, effective); err != nil {
		return err
	}
	if err := os.Chmod(tmp, effective); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
