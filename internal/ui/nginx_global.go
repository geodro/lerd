package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
)

// nginxGlobalBaseName is the file name of the http-level user override. We
// store it as a constant so the backup regex and the snapshot/write helpers
// share the same source of truth without re-deriving it from the path.
const nginxGlobalBaseName = "zz-lerd-user.conf"

// nginxGlobalBackupRe matches a fully-qualified global http override backup
// filename. Anchored at both ends so a partial-prefix match cannot be used
// as a path-traversal lever via the {name} URL segment.
var nginxGlobalBackupRe = regexp.MustCompile(`\A` + regexp.QuoteMeta(nginxGlobalBaseName) + `\.bkp\.\d{8}-\d{6}(-\d+)?\z`)

// uniqueGlobalNginxBackupPath returns a backup path inside dir that does not
// collide with an existing file. Mirrors uniqueNginxBackupPath for the global
// http config.
func uniqueGlobalNginxBackupPath(dir string, now time.Time) (string, error) {
	base := filepath.Join(dir, nginxGlobalBaseName+".bkp."+now.Format("20060102-150405"))
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base, nil
	}
	for i := 1; i < 1_000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("backup name space exhausted for global nginx in %s", dir)
}

// writeGlobalNginxBackup stages snap.data into NginxHttpDBkp with a unique
// timestamped name. Returns the absolute backup path (for rollback cleanup)
// and the base name for the API response.
func writeGlobalNginxBackup(snap nginxSnapshot, now time.Time) (string, string, error) {
	if !snap.existed {
		return "", "", nil
	}
	dir := config.NginxHttpDBkp()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("creating backup dir: %w", err)
	}
	backupPath, err := uniqueGlobalNginxBackupPath(dir, now)
	if err != nil {
		return "", "", err
	}
	if err := writeFileAtomic(backupPath, snap.data, snap.mode); err != nil {
		return "", "", fmt.Errorf("writing backup: %w", err)
	}
	return backupPath, filepath.Base(backupPath), nil
}

func listGlobalNginxBackups() ([]SiteNginxBackup, error) {
	dir := config.NginxHttpDBkp()
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
		if !nginxGlobalBackupRe.MatchString(name) {
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

// writeGlobalNginxOverride stages the new bytes via a temp file outside
// http.d/ (in http.d.bkp/) and renames the result into place. Mirrors
// writeNginxOverride for the global file so partial writes can never be
// picked up by the http.d/*.conf include glob.
func writeGlobalNginxOverride(confPath string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(config.NginxHttpD(), 0o755); err != nil {
		return fmt.Errorf("creating http.d: %w", err)
	}
	stageDir := config.NginxHttpDBkp()
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
// reuse the same dirty/backup/validation rendering. ValidationOutput carries
// the raw nginx -t stderr the modal renders inline on a failed save.
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

// handleNginxRoutes dispatches the global /api/nginx/* surface. We use a
// single registered prefix so /api/nginx/config, /api/nginx/backups,
// /api/nginx/restore and /api/nginx/reset all flow through one entry point.
func handleNginxRoutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/nginx/")
	switch {
	case rest == "config":
		handleNginxConfig(w, r)
	case rest == "backups":
		handleNginxConfigBackups(w, r)
	case strings.HasPrefix(rest, "backups/"):
		name := strings.TrimPrefix(rest, "backups/")
		handleNginxConfigBackupContent(w, r, name)
	case rest == "restore":
		handleNginxConfigRestore(w, r)
	case rest == "reset":
		handleNginxConfigReset(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handleNginxConfigBackups lists the global http override backups, newest
// first. Mirrors handleSiteNginxBackups.
func handleNginxConfigBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := listGlobalNginxBackups()
	if err != nil {
		http.Error(w, "listing backups: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []SiteNginxBackup{}
	}
	writeJSON(w, list)
}

// handleNginxConfigBackupContent serves the raw bytes of a single backup so
// the restore modal can show a diff before the user accepts. Mirrors
// handleSiteNginxBackupContent.
func handleNginxConfigBackupContent(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !nginxGlobalBackupRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(filepath.Join(config.NginxHttpDBkp(), name))
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

// handleNginxConfigReset deletes the global http override file so the
// generated nginx.conf falls back to lerd's bundled defaults. Backups are
// preserved in http.d.bkp/ so a Restore can recover from an accidental
// reset.
func handleNginxConfigReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	nginxSaveMu.Lock()
	defer nginxSaveMu.Unlock()
	path := config.NginxHttpUserConf()
	removeErr := os.Remove(path)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		writeJSON(w, NginxConfigResetResponse{OK: false, Error: removeErr.Error()})
		return
	}
	// Reload even when the file was already missing — running nginx may still
	// have the directives loaded from a previous lifetime (out-of-band rm,
	// crash mid-write, stale bind mount) and Reset is the user's signal that
	// they want the running config in sync with the empty disk state.
	if err := nginxReloadFn(); err != nil {
		writeJSON(w, NginxConfigResetResponse{OK: false, Error: "removed, but nginx reload failed: " + err.Error()})
		return
	}
	writeJSON(w, NginxConfigResetResponse{OK: true})
}

// handleNginxConfigRestore restores a named backup over the live global
// override and reloads nginx. Mirrors handleSiteNginxRestore.
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
	if !nginxGlobalBackupRe.MatchString(req.Name) {
		writeJSON(w, NginxConfigRestoreResponse{OK: false, Error: "invalid backup name"})
		return
	}
	nginxSaveMu.Lock()
	defer nginxSaveMu.Unlock()
	backupPath := filepath.Join(config.NginxHttpDBkp(), req.Name)
	backupInfo, statErr := os.Stat(backupPath)
	if statErr != nil {
		writeJSON(w, NginxConfigRestoreResponse{OK: false, Error: "stat backup: " + statErr.Error()})
		return
	}
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		writeJSON(w, NginxConfigRestoreResponse{OK: false, Error: "reading backup: " + err.Error()})
		return
	}
	confPath := config.NginxHttpUserConf()
	mode := backupInfo.Mode().Perm()
	if info, err := os.Stat(confPath); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(config.NginxHttpD(), 0o755); err != nil {
		writeJSON(w, NginxConfigRestoreResponse{OK: false, Error: "creating http.d: " + err.Error()})
		return
	}
	if err := writeFileAtomic(confPath, backupData, mode); err != nil {
		writeJSON(w, NginxConfigRestoreResponse{OK: false, Error: err.Error()})
		return
	}
	if err := nginxReloadFn(); err != nil {
		writeJSON(w, NginxConfigRestoreResponse{OK: false, Error: "restored, but nginx reload failed: " + err.Error(), Restored: req.Name, Content: string(backupData)})
		return
	}
	writeJSON(w, NginxConfigRestoreResponse{OK: true, Restored: req.Name, Content: string(backupData)})
}

// handleNginxConfig reads (GET) or saves (POST) the global http-level nginx
// override. The save path mirrors handleSiteNginx: snapshot, optional backup,
// off-glob staged write, `nginx -t` pre-flight, rollback on validation
// failure, reload on success.
func handleNginxConfig(w http.ResponseWriter, r *http.Request) {
	path := config.NginxHttpUserConf()
	if r.Method == http.MethodGet {
		body, err := os.ReadFile(path)
		exists := err == nil
		if err != nil {
			if !os.IsNotExist(err) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			body = []byte(nginxHttpTemplate)
		}
		writeJSON(w, NginxConfigReadResponse{Path: path, Content: string(body), Exists: exists})
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

	// Serialize the whole pipeline — including the heal step — under the
	// nginx save mutex. EnsureNginxConfig and RewriteNginxQuadlet are
	// non-atomic file writers; running them in parallel would race the
	// nginx.conf rewrite and the quadletChanged signal across two concurrent
	// saves.
	nginxSaveMu.Lock()
	defer nginxSaveMu.Unlock()

	// Heal preconditions for installs predating this feature: rerender
	// nginx.conf (which now carries the `include /etc/nginx/http.d/*.conf`
	// line) and rewrite the lerd-nginx quadlet from the bundled template
	// (which now carries the http.d Volume= mount).
	if err := nginx.EnsureNginxConfig(); err != nil {
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "ensuring nginx config: " + err.Error()})
		return
	}
	quadletChanged, err := nginx.RewriteNginxQuadlet()
	if err != nil {
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: "rewriting nginx quadlet: " + err.Error()})
		return
	}

	snap, err := readNginxSnapshot(path)
	if err != nil {
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: err.Error()})
		return
	}
	backupPath := ""
	backupName := ""
	if req.Backup {
		bp, bn, err := writeGlobalNginxBackup(snap, time.Now())
		if err != nil {
			writeJSON(w, NginxConfigWriteResponse{OK: false, Error: err.Error()})
			return
		}
		backupPath = bp
		backupName = bn
	}
	if err := writeGlobalNginxOverride(path, []byte(req.Content), snap.mode); err != nil {
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		writeJSON(w, NginxConfigWriteResponse{OK: false, Error: err.Error()})
		return
	}

	// A new Volume= mount only takes effect on container (re)start. We can't
	// usefully run `nginx -t` against the running container because its mount
	// view doesn't include the file we just wrote yet, so we go straight to a
	// restart. If the restart fails (typically because the bytes are broken),
	// roll back to the snapshot and restart on the known-good config so the
	// user isn't left with a poisoned override that breaks every subsequent
	// reload anywhere in lerd.
	if quadletChanged {
		_ = podman.DaemonReloadFn()
		if restartErr := podman.RestartUnit("lerd-nginx"); restartErr != nil {
			if rbErr := restoreNginxSnapshot(path, snap); rbErr != nil {
				writeJSON(w, NginxConfigWriteResponse{
					OK:         false,
					Error:      "nginx restart failed and rollback failed: " + rbErr.Error() + " (restart error: " + restartErr.Error() + ")",
					BackupName: backupName,
					Content:    req.Content,
					Exists:     true,
				})
				return
			}
			if rb2Err := podman.RestartUnit("lerd-nginx"); rb2Err != nil {
				writeJSON(w, NginxConfigWriteResponse{
					OK:    false,
					Error: "nginx config invalid and rollback restart failed: " + rb2Err.Error() + " (original: " + restartErr.Error() + ")",
				})
				return
			}
			if backupPath != "" {
				_ = os.Remove(backupPath)
			}
			writeJSON(w, NginxConfigWriteResponse{
				OK:    false,
				Error: "nginx config invalid, rolled back to previous contents: " + restartErr.Error(),
			})
			return
		}
		writeJSON(w, NginxConfigWriteResponse{
			OK:         true,
			BackupName: backupName,
			Content:    req.Content,
			Exists:     true,
		})
		return
	}

	output, testErr := nginxTestFn()
	if testErr != nil && validationMentionsOurFile(output, path) {
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		if rbErr := restoreNginxSnapshot(path, snap); rbErr != nil {
			writeJSON(w, NginxConfigWriteResponse{
				OK:               false,
				Error:            "nginx config invalid and rollback failed: " + rbErr.Error(),
				ValidationOutput: output,
			})
			return
		}
		writeJSON(w, NginxConfigWriteResponse{
			OK:               false,
			Error:            "nginx config invalid, rolled back to previous contents",
			ValidationOutput: output,
		})
		return
	}
	if err := nginxReloadFn(); err != nil {
		writeJSON(w, NginxConfigWriteResponse{
			OK:               false,
			Error:            "saved, but nginx reload failed: " + err.Error(),
			BackupName:       backupName,
			ValidationOutput: output,
			Content:          req.Content,
			Exists:           true,
		})
		return
	}
	writeJSON(w, NginxConfigWriteResponse{
		OK:               true,
		BackupName:       backupName,
		ValidationOutput: output,
		Content:          req.Content,
		Exists:           true,
	})
}
