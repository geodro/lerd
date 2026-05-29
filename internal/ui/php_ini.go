package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/cfgedit"
	"github.com/geodro/lerd/internal/config"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
)

// phpIniFile is the cfgedit.File for a version's user php.ini override. Backups
// and write-staging live in the version's ini.bkp/ dir, kept off the scan
// directory's top-level *.ini glob that FPM loads.
func phpIniFile(version string) cfgedit.File {
	return cfgedit.File{
		Path:     config.PHPUserIniFile(version),
		BkpDir:   config.PHPUserIniBkpDir(version),
		BkpName:  "98-user.ini",
		Template: phpUserIniTemplate,
	}
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

// PhpIniWriteResponse mirrors SiteNginxWriteResponse (minus ValidationOutput,
// which has no clean php pre-flight equivalent).
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
// flow needs after touching disk. WriteFPMQuadlet internally seeds the user ini
// via EnsureUserIni, which is why the reset path uses restartFPMUnit instead.
func fpmRestartForVersion(version string) error {
	if err := podman.WriteFPMQuadlet(version); err != nil {
		return fmt.Errorf("updating php quadlet: %w", err)
	}
	return restartFPMUnit(version)
}

// restartFPMUnit restarts the FPM container without touching the on-disk user
// ini. Used by the reset path, which has just deleted the file and would
// otherwise see it re-seeded by WriteFPMQuadlet → EnsureUserIni.
func restartFPMUnit(version string) error {
	short := strings.ReplaceAll(version, ".", "")
	return podman.RestartUnit("lerd-php" + short + "-fpm")
}

// phpUserIniTemplate seeds the GET handler when the user-ini does not exist
// yet. Matches the stub EnsureUserIni would write so the editor shows the same
// guidance.
const phpUserIniTemplate = `; Lerd per-version PHP settings.
;
; Edit this file, then click Save to write it and restart FPM.
;
; memory_limit = 512M
; opcache.memory_consumption = 256
; realpath_cache_size = 4096k
; realpath_cache_ttl = 600
`

// handlePhpIniConfig reads (GET) or saves (POST) a version's user php.ini.
// The save is bespoke: php.ini has no clean pre-flight, so we write, restart
// FPM, and roll back to the snapshot if the restart fails — leaving the user
// on a known-good config. Backups/snapshot/staging come from cfgedit.
func handlePhpIniConfig(w http.ResponseWriter, r *http.Request, version string) {
	installed, _ := phpPkg.ListInstalled()
	if !slices.Contains(installed, version) {
		http.NotFound(w, r)
		return
	}
	f := phpIniFile(version)
	if r.Method == http.MethodGet {
		got, err := f.Read()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, PhpIniReadResponse{Path: got.Path, Content: got.Body, Exists: got.Exists})
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

	cfgedit.Mu.Lock()
	defer cfgedit.Mu.Unlock()

	snap, err := cfgedit.ReadSnapshot(f.Path)
	if err != nil {
		writeJSON(w, PhpIniWriteResponse{OK: false, Error: err.Error()})
		return
	}
	backupPath, backupName := "", ""
	if req.Backup {
		bp, bn, err := f.WriteBackup(snap, time.Now())
		if err != nil {
			writeJSON(w, PhpIniWriteResponse{OK: false, Error: err.Error()})
			return
		}
		backupPath, backupName = bp, bn
	}
	if err := f.StagedWrite([]byte(req.Content), snap.Mode); err != nil {
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		writeJSON(w, PhpIniWriteResponse{OK: false, Error: err.Error()})
		return
	}
	if err := fpmRestartForVersion(version); err != nil {
		if rbErr := cfgedit.RestoreSnapshot(f.Path, snap); rbErr != nil {
			writeJSON(w, PhpIniWriteResponse{OK: false, Error: "saved, but FPM restart failed and rollback failed: " + rbErr.Error() + " (restart error: " + err.Error() + ")", BackupName: backupName, Content: req.Content, Exists: true})
			return
		}
		if rb2Err := fpmRestartForVersion(version); rb2Err != nil {
			writeJSON(w, PhpIniWriteResponse{OK: false, Error: "php.ini rejected and rollback restart also failed: " + rb2Err.Error() + " (original: " + err.Error() + ")"})
			return
		}
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		writeJSON(w, PhpIniWriteResponse{OK: false, Error: "php.ini rejected, rolled back: " + err.Error()})
		return
	}
	writeJSON(w, PhpIniWriteResponse{OK: true, BackupName: backupName, Content: req.Content, Exists: true})
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
	list, err := phpIniFile(version).ListBackups()
	if err != nil {
		http.Error(w, "listing backups: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []cfgedit.Backup{}
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
	data, err := phpIniFile(version).ReadBackup(name)
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
	// Restart via restartFPMUnit, not fpmRestartForVersion: the latter routes
	// through WriteFPMQuadlet → EnsureUserIni, which would re-seed the file we
	// just deleted. cfgedit.Reset skips the restart when nothing was removed.
	if err := phpIniFile(version).Reset(func() error { return restartFPMUnit(version) }); err != nil {
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
	f := phpIniFile(version)
	if !f.ValidBackupName(req.Name) {
		writeJSON(w, PhpIniRestoreResponse{OK: false, Error: "invalid backup name"})
		return
	}
	res, err := f.Restore(req.Name, func() error { return fpmRestartForVersion(version) })
	if err != nil {
		writeJSON(w, PhpIniRestoreResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, PhpIniRestoreResponse(res))
}
