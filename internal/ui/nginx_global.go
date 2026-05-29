package ui

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/cfgedit"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteops"
)

// globalNginxFile is the cfgedit.File for the http-level user override. Backups
// and write-staging live in http.d.bkp/, kept off the http.d/*.conf glob.
func globalNginxFile() cfgedit.File {
	return cfgedit.File{
		Path:     config.NginxHttpUserConf(),
		BkpDir:   config.NginxHttpDBkp(),
		BkpName:  "zz-lerd-user.conf",
		Template: nginxHttpTemplate,
	}
}

// NginxConfigReadResponse mirrors SiteNginxReadResponse for the global
// http-level override. Exists distinguishes a real saved override from the
// seeded template the handler hands back when the file is missing.
type NginxConfigReadResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Exists  bool   `json:"exists"`
}

// NginxConfigWriteRequest is the JSON body for POST /api/nginx/config.
type NginxConfigWriteRequest struct {
	Content string `json:"content"`
	Backup  bool   `json:"backup"`
}

// NginxConfigWriteResponse mirrors SiteNginxWriteResponse so the editor can
// reuse the same dirty/backup/validation rendering.
type NginxConfigWriteResponse struct {
	OK               bool   `json:"ok"`
	Error            string `json:"error,omitempty"`
	BackupName       string `json:"backup_name,omitempty"`
	ValidationOutput string `json:"validation_output,omitempty"`
	Content          string `json:"content,omitempty"`
	Exists           bool   `json:"exists,omitempty"`
}

// NginxConfigResetResponse mirrors SiteNginxResetResponse for the reset flow.
type NginxConfigResetResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// NginxConfigRestoreRequest names the backup to roll back to.
type NginxConfigRestoreRequest struct {
	Name string `json:"name"`
}

// NginxConfigRestoreResponse mirrors SiteNginxRestoreResponse.
type NginxConfigRestoreResponse struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Restored string `json:"restored,omitempty"`
	Content  string `json:"content,omitempty"`
}

// handleNginxRoutes dispatches the global /api/nginx/* surface through one
// registered prefix.
func handleNginxRoutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/nginx/")
	switch {
	case rest == "config":
		handleNginxConfig(w, r)
	case rest == "backups":
		handleNginxConfigBackups(w, r)
	case strings.HasPrefix(rest, "backups/"):
		handleNginxConfigBackupContent(w, r, strings.TrimPrefix(rest, "backups/"))
	case rest == "restore":
		handleNginxConfigRestore(w, r)
	case rest == "reset":
		handleNginxConfigReset(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleNginxConfigBackups lists the global http override backups, newest first.
func handleNginxConfigBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := globalNginxFile().ListBackups()
	if err != nil {
		http.Error(w, "listing backups: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []cfgedit.Backup{}
	}
	writeJSON(w, list)
}

// handleNginxConfigBackupContent serves the raw bytes of a single backup so the
// restore modal can show a diff before the user accepts.
func handleNginxConfigBackupContent(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := globalNginxFile().ReadBackup(name)
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

// handleNginxConfigReset deletes the global http override so nginx.conf falls
// back to lerd's bundled defaults, then reloads. Unlike a per-site reset it
// reloads even when the file was already missing: a running nginx may still
// hold directives from a previous lifetime (out-of-band rm, crash, stale
// mount) and Reset is the user's signal to sync the runtime to the empty disk.
func handleNginxConfigReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfgedit.Mu.Lock()
	defer cfgedit.Mu.Unlock()
	if err := os.Remove(config.NginxHttpUserConf()); err != nil && !os.IsNotExist(err) {
		writeJSON(w, NginxConfigResetResponse{OK: false, Error: err.Error()})
		return
	}
	if err := siteops.NginxReloadFn(); err != nil {
		writeJSON(w, NginxConfigResetResponse{OK: false, Error: "removed, but nginx reload failed: " + err.Error()})
		return
	}
	writeJSON(w, NginxConfigResetResponse{OK: true})
}

// handleNginxConfigRestore restores a named backup over the live global
// override and reloads nginx.
func handleNginxConfigRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req NginxConfigRestoreRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeJSON(w, NginxConfigRestoreResponse{OK: false, Error: "invalid body: " + err.Error()})
		return
	}
	f := globalNginxFile()
	if !f.ValidBackupName(req.Name) {
		writeJSON(w, NginxConfigRestoreResponse{OK: false, Error: "invalid backup name"})
		return
	}
	res, err := f.Restore(req.Name, func() error { return siteops.NginxReloadFn() })
	if err != nil {
		writeJSON(w, NginxConfigRestoreResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, NginxConfigRestoreResponse(res))
}

// handleNginxConfig reads (GET) or saves (POST) the global http-level override.
// The save path is bespoke: a new http.d Volume= mount only takes effect on
// container restart, so when the quadlet changes we restart (rolling back on
// failure) instead of running `nginx -t` against a mount the running container
// can't see yet. Otherwise it is the standard validate + reload via cfgedit.
func handleNginxConfig(w http.ResponseWriter, r *http.Request) {
	f := globalNginxFile()
	if r.Method == http.MethodGet {
		got, err := f.Read()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, NginxConfigReadResponse{Path: got.Path, Content: got.Body, Exists: got.Exists})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req NginxConfigWriteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "invalid body: " + err.Error()})
		return
	}

	cfgedit.Mu.Lock()
	defer cfgedit.Mu.Unlock()

	// Heal preconditions for installs predating this feature: rerender
	// nginx.conf (now carrying the http.d include) and the lerd-nginx quadlet
	// (now carrying the http.d Volume= mount).
	if err := nginx.EnsureNginxConfig(); err != nil {
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "ensuring nginx config: " + err.Error()})
		return
	}
	quadletChanged, err := nginx.RewriteNginxQuadlet()
	if err != nil {
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "rewriting nginx quadlet: " + err.Error()})
		return
	}

	snap, err := cfgedit.ReadSnapshot(f.Path)
	if err != nil {
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: err.Error()})
		return
	}
	backupPath, backupName := "", ""
	if req.Backup {
		bp, bn, err := f.WriteBackup(snap, time.Now())
		if err != nil {
			writeJSON(w, NginxConfigWriteResponse{OK: false, Error: err.Error()})
			return
		}
		backupPath, backupName = bp, bn
	}
	if err := f.StagedWrite([]byte(req.Content), snap.Mode); err != nil {
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: err.Error()})
		return
	}

	if quadletChanged {
		_ = podman.DaemonReloadFn()
		if restartErr := podman.RestartUnit("lerd-nginx"); restartErr != nil {
			if rbErr := cfgedit.RestoreSnapshot(f.Path, snap); rbErr != nil {
				writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "nginx restart failed and rollback failed: " + rbErr.Error() + " (restart error: " + restartErr.Error() + ")", BackupName: backupName, Content: req.Content, Exists: true})
				return
			}
			if rb2Err := podman.RestartUnit("lerd-nginx"); rb2Err != nil {
				writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "nginx config invalid and rollback restart failed: " + rb2Err.Error() + " (original: " + restartErr.Error() + ")"})
				return
			}
			if backupPath != "" {
				_ = os.Remove(backupPath)
			}
			writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "nginx config invalid, rolled back to previous contents: " + restartErr.Error()})
			return
		}
		writeJSON(w, NginxConfigWriteResponse{OK: true, BackupName: backupName, Content: req.Content, Exists: true})
		return
	}

	output, testErr := siteops.NginxTestFn()
	if testErr != nil && cfgedit.MentionsFile(output, f.Path) {
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		if rbErr := cfgedit.RestoreSnapshot(f.Path, snap); rbErr != nil {
			writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "nginx config invalid and rollback failed: " + rbErr.Error(), ValidationOutput: output})
			return
		}
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "config invalid, rolled back to previous contents", ValidationOutput: output})
		return
	}
	if err := siteops.NginxReloadFn(); err != nil {
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "saved, but nginx reload failed: " + err.Error(), BackupName: backupName, ValidationOutput: output, Content: req.Content, Exists: true})
		return
	}
	writeJSON(w, NginxConfigWriteResponse{OK: true, BackupName: backupName, ValidationOutput: output, Content: req.Content, Exists: true})
}
