package ui

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// handleSiteAction routes GET /api/sites/{domain}/env to handleSiteEnv and
// returns the raw .env contents verbatim, preserving comments and ordering
// so the UI can show the file as-is.
func TestHandleSiteEnv_returnsRawContents(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	envBody := "# header comment\nDB_HOST=127.0.0.1\nDB_PORT=3306\n\nMAIL_HOST=mailhog\n"
	if err := os.WriteFile(filepath.Join(sitePath, ".env"), []byte(envBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/env", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != envBody {
		t.Errorf("body mismatch\n got: %q\nwant: %q", got, envBody)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type: got %q want text/plain; charset=utf-8", ct)
	}
}

// Missing .env returns 200 with an empty body so the UI's gate falls back
// gracefully instead of producing a noisy 404 in the network panel.
func TestHandleSiteEnv_missingFileReturnsEmptyBody(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := config.AddSite(config.Site{Name: "noenv", Path: t.TempDir(), Domains: []string{"noenv.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/noenv.test/env", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("expected empty body, got %q", rec.Body.String())
	}
}

// Only GET (read) and PUT (write) are valid on /env. POST and friends stay
// 405 so a future shared dispatcher cannot quietly widen the contract.
func TestHandleSiteEnv_postStillRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/env", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleSiteEnv_putWritesNewFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(SiteEnvWriteRequest{Content: "APP_KEY=base64:abc\nDB_HOST=127.0.0.1\n", Backup: false})
	req := httptest.NewRequest(http.MethodPut, "/api/sites/acme.test/env", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteEnvWriteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("ok=false: %q", resp.Error)
	}
	if resp.BackupPath != "" {
		t.Errorf("BackupPath: got %q want \"\" when backup=false", resp.BackupPath)
	}
	got, err := os.ReadFile(filepath.Join(sitePath, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "APP_KEY=base64:abc\nDB_HOST=127.0.0.1\n" {
		t.Errorf("file body mismatch: got %q", string(got))
	}
}

func TestHandleSiteEnv_putPreservesMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	envPath := filepath.Join(sitePath, ".env")
	if err := os.WriteFile(envPath, []byte("OLD=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(SiteEnvWriteRequest{Content: "NEW=2\n", Backup: false})
	req := httptest.NewRequest(http.MethodPut, "/api/sites/acme.test/env", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fs.FileMode(0o600) {
		t.Errorf("mode: got %o want 0600", info.Mode().Perm())
	}
}

func TestHandleSiteEnv_putCreatesBackup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	envPath := filepath.Join(sitePath, ".env")
	oldBody := "DB_PASSWORD=hunter2\n"
	if err := os.WriteFile(envPath, []byte(oldBody), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(SiteEnvWriteRequest{Content: "DB_PASSWORD=correcthorse\n", Backup: true})
	req := httptest.NewRequest(http.MethodPut, "/api/sites/acme.test/env", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteEnvWriteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("ok=false: %q", resp.Error)
	}
	if resp.BackupPath == "" {
		t.Fatal("expected BackupPath set when backup=true")
	}
	if !strings.HasPrefix(resp.BackupPath, ".env.bkp.") {
		t.Errorf("BackupPath %q does not start with .env.bkp.", resp.BackupPath)
	}
	bak, err := os.ReadFile(filepath.Join(sitePath, resp.BackupPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != oldBody {
		t.Errorf("backup body mismatch: got %q want %q", string(bak), oldBody)
	}
	info, err := os.Stat(filepath.Join(sitePath, resp.BackupPath))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fs.FileMode(0o640) {
		t.Errorf("backup mode: got %o want 0640", info.Mode().Perm())
	}
}

// withCORS must advertise PUT alongside GET and POST so that browser
// preflights for the env-write endpoint do not strip the actual request.
// Regression guard for "Failed to fetch" on Save in the dashboard.
func TestWithCORS_advertisesPUT(t *testing.T) {
	handler := withCORS(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodOptions, "/api/sites/acme.test/env", nil)
	req.Header.Set("Origin", "http://lerd.localhost")
	rec := httptest.NewRecorder()
	handler(rec, req)

	methods := rec.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(methods, "PUT") {
		t.Errorf("Allow-Methods does not include PUT: %q", methods)
	}
}

func TestListEnvFiles_returnsEnvVariantsWithDefaultFirst(t *testing.T) {
	dir := t.TempDir()
	must := func(name string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), mode); err != nil {
			t.Fatal(err)
		}
	}
	must(".env", 0o644)
	must(".env.testing", 0o644)
	must(".env.local", 0o644)
	must(".env.example", 0o644)
	must(".env.bkp.20260528-103045", 0o644)         // backup of .env, excluded
	must(".env.testing.bkp.20260528-103045", 0o644) // backup of .env.testing, excluded
	must(".env.before_lerd", 0o644)                 // lerd's own restore file, excluded
	must(".env.tmp.abc", 0o644)                     // matches via two-segment, excluded by regex
	must("regular.txt", 0o644)                      // not an env file

	got, err := listEnvFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".env", ".env.example", ".env.local", ".env.testing"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestEnvFileFromQuery(t *testing.T) {
	cases := []struct {
		q        string
		wantFile string
		wantOK   bool
	}{
		{"", ".env", true},
		{"file=.env", ".env", true},
		{"file=.env.testing", ".env.testing", true},
		{"file=.env.local", ".env.local", true},
		{"file=.env.before_lerd", "", false},
		{"file=.env.bkp.20260528-103045", "", false}, // backup, two-segment suffix
		{"file=../etc/passwd", "", false},
		{"file=.env/extra", "", false},
		{"file=other.txt", "", false},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/?"+c.q, nil)
		gotFile, gotOK := envFileFromQuery(req)
		if gotOK != c.wantOK {
			t.Errorf("q=%q ok: got %v want %v", c.q, gotOK, c.wantOK)
		}
		if gotOK && gotFile != c.wantFile {
			t.Errorf("q=%q file: got %q want %q", c.q, gotFile, c.wantFile)
		}
	}
}

func TestHandleSiteEnv_filesListAndPerFileReadWrite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(sitePath, ".env"), []byte("APP=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sitePath, ".env.testing"), []byte("TEST=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	// List
	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/env/files", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", rec.Code, rec.Body.String())
	}
	var files []string
	if err := json.Unmarshal(rec.Body.Bytes(), &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != ".env" || files[1] != ".env.testing" {
		t.Errorf("file list: got %v", files)
	}

	// Read with file=.env.testing
	req = httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/env?file=.env.testing", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec = httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read status %d", rec.Code)
	}
	if got := rec.Body.String(); got != "TEST=1\n" {
		t.Errorf("read body: got %q want %q", got, "TEST=1\n")
	}

	// Write to .env.testing with backup
	body, _ := json.Marshal(SiteEnvWriteRequest{Content: "TEST=2\n", Backup: true})
	req = httptest.NewRequest(http.MethodPut, "/api/sites/acme.test/env?file=.env.testing", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:54321"
	rec = httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("write status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteEnvWriteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("ok=false: %q", resp.Error)
	}
	if !strings.HasPrefix(resp.BackupPath, ".env.testing.bkp.") {
		t.Errorf("BackupPath: got %q want prefix .env.testing.bkp.", resp.BackupPath)
	}
	// Original .env must not have been touched.
	got, err := os.ReadFile(filepath.Join(sitePath, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "APP=1\n" {
		t.Errorf(".env contaminated: %q", string(got))
	}
}

func TestHandleSiteEnv_restoreIsScopedToFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(sitePath, ".env"), []byte("NEW=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sitePath, ".env.testing"), []byte("TNEW=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Two backups: one of .env, one of .env.testing.
	if err := os.WriteFile(filepath.Join(sitePath, ".env.bkp.20260528-103045"), []byte("OLD=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sitePath, ".env.testing.bkp.20260528-103045"), []byte("TOLD=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/env/restore?file=.env.testing", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteEnvRestoreResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("ok=false: %q", resp.Error)
	}
	if resp.Restored != ".env.testing.bkp.20260528-103045" {
		t.Errorf("Restored: got %q", resp.Restored)
	}
	// .env.testing reverted, .env intact, .env backup untouched.
	got, _ := os.ReadFile(filepath.Join(sitePath, ".env.testing"))
	if string(got) != "TOLD=1\n" {
		t.Errorf(".env.testing: got %q want TOLD=1", string(got))
	}
	got, _ = os.ReadFile(filepath.Join(sitePath, ".env"))
	if string(got) != "NEW=2\n" {
		t.Errorf(".env touched: got %q", string(got))
	}
	if _, err := os.Stat(filepath.Join(sitePath, ".env.bkp.20260528-103045")); err != nil {
		t.Errorf(".env backup gone: %v", err)
	}
}

func TestHandleSiteEnv_restoreUsesMostRecentBackup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(sitePath, ".env"), []byte("NEW=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sitePath, ".env.bkp.20260101-100000"), []byte("ANCIENT=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sitePath, ".env.bkp.20260528-103045"), []byte("OLD=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/env/restore", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteEnvRestoreResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK {
		t.Fatalf("ok=false: %q", resp.Error)
	}
	if resp.Restored != ".env.bkp.20260528-103045" {
		t.Errorf("Restored: got %q want %q", resp.Restored, ".env.bkp.20260528-103045")
	}
	if resp.Content != "OLD=1\n" {
		t.Errorf("Content: got %q want %q", resp.Content, "OLD=1\n")
	}
	// Ancient backup should still be on disk; only the restored one is removed.
	if _, err := os.Stat(filepath.Join(sitePath, ".env.bkp.20260101-100000")); err != nil {
		t.Errorf("ancient backup gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sitePath, ".env.bkp.20260528-103045")); !os.IsNotExist(err) {
		t.Errorf("restored backup not removed: err=%v", err)
	}
}

func TestHandleSiteEnv_restoreWithoutBackupReturnsError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(sitePath, ".env"), []byte("X=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/env/restore", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var resp SiteEnvRestoreResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.OK {
		t.Error("expected ok=false when no backup exists")
	}
	if !strings.Contains(resp.Error, "no backup") {
		t.Errorf("error: got %q want substring 'no backup'", resp.Error)
	}
}

func TestHandleSiteEnv_backupContentServesRawBytes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	body := "OLD=1\n"
	if err := os.WriteFile(filepath.Join(sitePath, ".env.bkp.20260528-103045"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/env/backups/.env.bkp.20260528-103045", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != body {
		t.Errorf("body: got %q want %q", got, body)
	}
}

func TestHandleSiteEnv_backupContentRejectsTraversalAndOtherNames(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(sitePath, ".env.before_lerd"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	// Path traversal: the / segment makes parts longer than 4, falling
	// through to the no-match branch.
	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/env/backups/.env.before_lerd", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("non-backup name: got %d want 404", rec.Code)
	}
}

func TestHandleSiteEnv_backupsListsNewestFirst(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(sitePath, ".env.bkp.20260101-100000"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sitePath, ".env.bkp.20260528-103045"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/env/backups", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var list []SiteEnvBackup
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len: got %d want 2", len(list))
	}
	if list[0].Name != ".env.bkp.20260528-103045" {
		t.Errorf("newest: got %q want %q", list[0].Name, ".env.bkp.20260528-103045")
	}
}

// siteHasEnv distinguishes "file present" from "directory present" so the
// UI only surfaces the Env tab for sites whose root has a real .env file.
func TestSiteHasEnv(t *testing.T) {
	dir := t.TempDir()
	if siteHasEnv(dir) {
		t.Error("expected false when .env missing")
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("X=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !siteHasEnv(dir) {
		t.Error("expected true after writing .env")
	}

	// A directory named .env (legal on disk) must not count as a usable env file.
	dirOnly := t.TempDir()
	if err := os.Mkdir(filepath.Join(dirOnly, ".env"), 0o755); err != nil {
		t.Fatal(err)
	}
	if siteHasEnv(dirOnly) {
		t.Error("expected false when .env is a directory")
	}
}
