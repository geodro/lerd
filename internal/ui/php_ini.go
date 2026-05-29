package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/config"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
)

// phpIniBaseName is the file name FPM picks up from the version's ini scan
// directory. Sharing the constant lets the backup regex and write helpers
// stay in lockstep with what FPM actually loads.
const phpIniBaseName = "98-user.ini"

// phpIniBackupRe matches a fully-qualified backup filename. Anchored at both
// ends so a partial-prefix match cannot be used as a path-traversal lever
// through the {name} URL segment.
var phpIniBackupRe = regexp.MustCompile(`\A` + regexp.QuoteMeta(phpIniBaseName) + `\.bkp\.\d{8}-\d{6}(-\d+)?\z`)

// phpIniSaveMu serializes save / reset / restore on the per-version php.ini
// override. The flow snapshots, writes, restarts FPM — and if two unsynchronized
// saves landed concurrently a failed restart could roll back the wrong bytes.
var phpIniSaveMu sync.Mutex

func uniquePhpIniBackupPath(dir string, now time.Time) (string, error) {
	base := filepath.Join(dir, phpIniBaseName+".bkp."+now.Format("20060102-150405"))
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base, nil
	}
	for i := 1; i < 1_000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("backup name space exhausted in %s", dir)
}

func writePhpIniBackup(version string, snap nginxSnapshot, now time.Time) (string, string, error) {
	if !snap.existed {
		return "", "", nil
	}
	dir := config.PHPUserIniBkpDir(version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("creating backup dir: %w", err)
	}
	backupPath, err := uniquePhpIniBackupPath(dir, now)
	if err != nil {
		return "", "", err
	}
	if err := writeFileAtomic(backupPath, snap.data, snap.mode); err != nil {
		return "", "", fmt.Errorf("writing backup: %w", err)
	}
	return backupPath, filepath.Base(backupPath), nil
}

func listPhpIniBackups(version string) ([]SiteNginxBackup, error) {
	dir := config.PHPUserIniBkpDir(version)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []SiteNginxBackup{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !phpIniBackupRe.MatchString(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, SiteNginxBackup{Name: name, MtimeUnix: info.ModTime().Unix()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// writePhpIniOverride stages the new bytes through a temp file living OUTSIDE
// the ini scan directory and renames the result into place. The scan dir
// includes only files with `.ini` extension at the top level, so a temp file
// named `98-user.ini.tmp.<n>` in the scan dir would briefly satisfy the glob;
// staging in ini.bkp/ and then renaming across keeps the half-written file
// invisible to FPM until the swap completes.
func writePhpIniOverride(confPath string, data []byte, mode os.FileMode) error {
	scanDir := filepath.Dir(confPath)
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		return fmt.Errorf("creating scan dir: %w", err)
	}
	stageDir := filepath.Join(scanDir, "ini.bkp")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return fmt.Errorf("creating stage dir: %w", err)
	}
	tmp, err := os.CreateTemp(stageDir, filepath.Base(confPath)+".tmp.*")
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
	if err := os.Rename(tmpPath, confPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming into place: %w", err)
	}
	return nil
}

// PhpIniReadResponse mirrors SiteNginxReadResponse. Exists distinguishes a
// real saved override from the seeded template the handler hands back when
// the file is missing.
type PhpIniReadResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Exists  bool   `json:"exists"`
}

// PhpIniWriteRequest is the JSON body for POST /api/php-versions/{v}/config.
type PhpIniWriteRequest struct {
	Content string `json:"content"`
	Backup  bool   `json:"backup"`
}

// PhpIniWriteResponse mirrors SiteNginxWriteResponse. ValidationOutput is not
// populated for php.ini (no clean pre-flight equivalent exists), but the
// field is kept so the frontend can share the modal rendering with nginx.
type PhpIniWriteResponse struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	BackupName string `json:"backup_name,omitempty"`
	Content    string `json:"content,omitempty"`
	Exists     bool   `json:"exists,omitempty"`
}

type PhpIniResetResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type PhpIniRestoreRequest struct {
	Name string `json:"name"`
}

type PhpIniRestoreResponse struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Restored string `json:"restored,omitempty"`
	Content  string `json:"content,omitempty"`
}

// fpmRestartForVersion encapsulates the quadlet+restart dance the ini-saving
// flow needs after touching disk. Returns nil on success; on failure the
// caller is expected to surface the error and may roll back the bytes.
// WriteFPMQuadlet internally seeds the user ini via EnsureUserIni, which is
// why the destructive reset path uses restartFPMUnit instead.
func fpmRestartForVersion(version string) error {
	if err := podman.WriteFPMQuadlet(version); err != nil {
		return fmt.Errorf("updating php quadlet: %w", err)
	}
	return restartFPMUnit(version)
}

// restartFPMUnit restarts the FPM container without touching the on-disk
// user ini. Used by the reset path, which has just deleted the file and
// would otherwise see it re-seeded by WriteFPMQuadlet → EnsureUserIni.
func restartFPMUnit(version string) error {
	short := strings.ReplaceAll(version, ".", "")
	return podman.RestartUnit("lerd-php" + short + "-fpm")
}

// phpUserIniTemplate is the seed content the GET handler returns when the
// user-ini file does not yet exist on disk (or was just reset). Matches the
// stub EnsureUserIni would write so the editor shows the same guidance.
const phpUserIniTemplate = `; Lerd per-version PHP settings.
;
; Edit this file, then click Save to write it and restart FPM.
;
; memory_limit = 512M
; opcache.memory_consumption = 256
; realpath_cache_size = 4096k
; realpath_cache_ttl = 600
`

// handlePhpIniConfig is the new save flow for /api/php-versions/{v}/config.
// Mirrors handleSiteNginx: snapshot, optional backup, off-glob staged write,
// restart FPM, rollback on restart failure.
func handlePhpIniConfig(w http.ResponseWriter, r *http.Request, version string) {
	installed, _ := phpPkg.ListInstalled()
	if !slices.Contains(installed, version) {
		http.NotFound(w, r)
		return
	}
	path := config.PHPUserIniFile(version)
	if r.Method == http.MethodGet {
		// Read on demand without re-seeding. EnsureUserIni would silently
		// recreate the file the user just reset (or never had), turning the
		// editor's exists=false signal into a lie.
		body, err := os.ReadFile(path)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !exists {
			body = []byte(phpUserIniTemplate)
		}
		writeJSON(w, PhpIniReadResponse{Path: path, Content: string(body), Exists: exists})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req PhpIniWriteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, PhpIniWriteResponse{OK: false, Error: "invalid body: " + err.Error()})
		return
	}

	phpIniSaveMu.Lock()
	defer phpIniSaveMu.Unlock()

	snap, err := readNginxSnapshot(path)
	if err != nil {
		writeJSON(w, PhpIniWriteResponse{OK: false, Error: err.Error()})
		return
	}
	backupPath := ""
	backupName := ""
	if req.Backup {
		bp, bn, err := writePhpIniBackup(version, snap, time.Now())
		if err != nil {
			writeJSON(w, PhpIniWriteResponse{OK: false, Error: err.Error()})
			return
		}
		backupPath = bp
		backupName = bn
	}
	if err := writePhpIniOverride(path, []byte(req.Content), snap.mode); err != nil {
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		writeJSON(w, PhpIniWriteResponse{OK: false, Error: err.Error()})
		return
	}
	if err := fpmRestartForVersion(version); err != nil {
		// FPM refused to start with the new ini. Roll back to the pre-write
		// snapshot and try the restart again so the user is left running on
		// a known-good config. Capture the second restart's error so we never
		// claim "rolled back" when FPM is actually still down.
		if rbErr := restoreNginxSnapshot(path, snap); rbErr != nil {
			writeJSON(w, PhpIniWriteResponse{
				OK:         false,
				Error:      "saved, but FPM restart failed and rollback failed: " + rbErr.Error() + " (restart error: " + err.Error() + ")",
				BackupName: backupName,
				Content:    req.Content,
				Exists:     true,
			})
			return
		}
		if rb2Err := fpmRestartForVersion(version); rb2Err != nil {
			writeJSON(w, PhpIniWriteResponse{
				OK:    false,
				Error: "php.ini rejected and rollback restart also failed: " + rb2Err.Error() + " (original: " + err.Error() + ")",
			})
			return
		}
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		writeJSON(w, PhpIniWriteResponse{
			OK:    false,
			Error: "php.ini rejected, rolled back: " + err.Error(),
		})
		return
	}
	writeJSON(w, PhpIniWriteResponse{
		OK:         true,
		BackupName: backupName,
		Content:    req.Content,
		Exists:     true,
	})
}

func handlePhpIniBackups(w http.ResponseWriter, r *http.Request, version string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	installed, _ := phpPkg.ListInstalled()
	if !slices.Contains(installed, version) {
		http.NotFound(w, r)
		return
	}
	list, err := listPhpIniBackups(version)
	if err != nil {
		http.Error(w, "listing backups: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []SiteNginxBackup{}
	}
	writeJSON(w, list)
}

func handlePhpIniBackupContent(w http.ResponseWriter, r *http.Request, version, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	installed, _ := phpPkg.ListInstalled()
	if !slices.Contains(installed, version) {
		http.NotFound(w, r)
		return
	}
	if !phpIniBackupRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(filepath.Join(config.PHPUserIniBkpDir(version), name))
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "reading backup: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func handlePhpIniReset(w http.ResponseWriter, r *http.Request, version string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	installed, _ := phpPkg.ListInstalled()
	if !slices.Contains(installed, version) {
		http.NotFound(w, r)
		return
	}
	phpIniSaveMu.Lock()
	defer phpIniSaveMu.Unlock()
	path := config.PHPUserIniFile(version)
	removeErr := os.Remove(path)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		writeJSON(w, PhpIniResetResponse{OK: false, Error: removeErr.Error()})
		return
	}
	if os.IsNotExist(removeErr) {
		writeJSON(w, PhpIniResetResponse{OK: true})
		return
	}
	// Bypass fpmRestartForVersion here: it routes through WriteFPMQuadlet →
	// EnsureUserIni, which would immediately re-seed the file we just deleted
	// and make `exists` come back true on the next GET. The user explicitly
	// asked for the override to be gone; just restart the unit.
	if err := restartFPMUnit(version); err != nil {
		writeJSON(w, PhpIniResetResponse{OK: false, Error: "removed, but FPM restart failed: " + err.Error()})
		return
	}
	writeJSON(w, PhpIniResetResponse{OK: true})
}

func handlePhpIniRestore(w http.ResponseWriter, r *http.Request, version string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	installed, _ := phpPkg.ListInstalled()
	if !slices.Contains(installed, version) {
		http.NotFound(w, r)
		return
	}
	var req PhpIniRestoreRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSON(w, PhpIniRestoreResponse{OK: false, Error: "invalid body: " + err.Error()})
		return
	}
	if !phpIniBackupRe.MatchString(req.Name) {
		writeJSON(w, PhpIniRestoreResponse{OK: false, Error: "invalid backup name"})
		return
	}
	phpIniSaveMu.Lock()
	defer phpIniSaveMu.Unlock()
	backupPath := filepath.Join(config.PHPUserIniBkpDir(version), req.Name)
	backupInfo, statErr := os.Stat(backupPath)
	if statErr != nil {
		writeJSON(w, PhpIniRestoreResponse{OK: false, Error: "stat backup: " + statErr.Error()})
		return
	}
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		writeJSON(w, PhpIniRestoreResponse{OK: false, Error: "reading backup: " + err.Error()})
		return
	}
	confPath := config.PHPUserIniFile(version)
	mode := backupInfo.Mode().Perm()
	if info, err := os.Stat(confPath); err == nil {
		mode = info.Mode().Perm()
	}
	scanDir := filepath.Dir(confPath)
	if err := os.MkdirAll(scanDir, 0o755); err != nil {
		writeJSON(w, PhpIniRestoreResponse{OK: false, Error: "creating scan dir: " + err.Error()})
		return
	}
	if err := writeFileAtomic(confPath, backupData, mode); err != nil {
		writeJSON(w, PhpIniRestoreResponse{OK: false, Error: err.Error()})
		return
	}
	if err := fpmRestartForVersion(version); err != nil {
		writeJSON(w, PhpIniRestoreResponse{
			OK:       false,
			Error:    "restored, but FPM restart failed: " + err.Error(),
			Restored: req.Name,
			Content:  string(backupData),
		})
		return
	}
	writeJSON(w, PhpIniRestoreResponse{OK: true, Restored: req.Name, Content: string(backupData)})
}
