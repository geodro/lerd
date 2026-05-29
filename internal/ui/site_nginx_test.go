package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/siteops"
)

// stubNginxReload swaps siteops.NginxReloadFn for the duration of a
// test. The real reload shells out to podman exec lerd-nginx, which is not
// available in CI; tests exercise the on-disk side effects of the handlers
// and assert that reload was invoked at the expected moments.
func stubNginxReload(t *testing.T) *int {
	t.Helper()
	calls := 0
	prev := siteops.NginxReloadFn
	siteops.NginxReloadFn = func() error {
		calls++
		return nil
	}
	t.Cleanup(func() { siteops.NginxReloadFn = prev })
	return &calls
}

// stubNginxTest swaps siteops.NginxTestFn for the duration of a
// test. By default `nginx -t` shells into the lerd-nginx container; tests
// here either want a quiet success (most common) or a controlled failure
// to exercise the rollback path.
func stubNginxTest(t *testing.T, output string, err error) *int {
	t.Helper()
	calls := 0
	prev := siteops.NginxTestFn
	siteops.NginxTestFn = func() (string, error) {
		calls++
		return output, err
	}
	t.Cleanup(func() { siteops.NginxTestFn = prev })
	return &calls
}

func TestHandleSiteNginx_getReturnsTemplateWhenMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubNginxReload(t)
	stubNginxTest(t, "nginx: configuration file /etc/nginx/nginx.conf test is successful", nil)

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/nginx", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteNginxReadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasSuffix(resp.Path, "/custom.d/acme.test.conf") {
		t.Errorf("path: got %q want suffix /custom.d/acme.test.conf", resp.Path)
	}
	if !strings.Contains(resp.Content, "Lerd per-site nginx overrides") {
		t.Errorf("expected template content, got %q", resp.Content)
	}
	if resp.Exists {
		t.Errorf("exists should be false when only the template is returned")
	}
}

// A worktree's nginx override is keyed by its subdomain, which is not a
// registered site domain — the handler must still resolve it (regression: it
// 404'd because FindSiteByDomain only knows primary/alias domains).
func TestHandleSiteNginx_getResolvesWorktreeDomain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubNginxReload(t)

	mainSite := filepath.Join(t.TempDir(), "acme")
	survivor := filepath.Join(t.TempDir(), "acme-feat")
	for _, d := range []string{filepath.Join(mainSite, ".git", "worktrees", "feat"), survivor} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	wtMeta := filepath.Join(mainSite, ".git", "worktrees", "feat")
	_ = os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/feat\n"), 0o644)
	_ = os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(survivor, ".git")+"\n"), 0o644)
	if err := config.AddSite(config.Site{Name: "acme", Path: mainSite, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/feat.acme.test/nginx", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("worktree nginx endpoint status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteNginxReadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasSuffix(resp.Path, "/custom.d/feat.acme.test.conf") {
		t.Errorf("path: got %q want the worktree override path", resp.Path)
	}
}

func TestHandleSiteNginx_getReportsExistsWhenFileSaved(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubNginxReload(t)

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	dir := config.NginxCustomD()
	_ = os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "acme.test.conf"), []byte("# saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/nginx", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	var resp SiteNginxReadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Exists {
		t.Errorf("exists should be true once a real file is on disk")
	}
	if resp.Content != "# saved\n" {
		t.Errorf("content: %q", resp.Content)
	}
}

func TestHandleSiteNginx_postWritesFileAndReloads(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	reloads := stubNginxReload(t)
	tests := stubNginxTest(t, "nginx -t ok", nil)

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(SiteNginxWriteRequest{Content: "client_max_body_size 100m;\n"})
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteNginxWriteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.OK || resp.BackupName != "" {
		t.Errorf("want ok with no backup, got %+v", resp)
	}
	if resp.ValidationOutput != "nginx -t ok" {
		t.Errorf("expected validation output to be surfaced, got %q", resp.ValidationOutput)
	}
	if *reloads != 1 {
		t.Errorf("reload calls: got %d want 1", *reloads)
	}
	if *tests != 1 {
		t.Errorf("nginx -t calls: got %d want 1", *tests)
	}
	data, err := os.ReadFile(filepath.Join(config.NginxCustomD(), "acme.test.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "client_max_body_size 100m;\n" {
		t.Errorf("written content: %q", data)
	}
}

func TestHandleSiteNginx_postWithBackupRollsExisting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubNginxReload(t)
	stubNginxTest(t, "ok", nil)

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	dir := config.NginxCustomD()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acme.test.conf"), []byte("# original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(SiteNginxWriteRequest{Content: "# new\n", Backup: true})
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteNginxWriteResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK || resp.BackupName == "" {
		t.Fatalf("want ok with backup name, got %+v", resp)
	}
	if !siteops.ValidNginxBackupName("acme.test", resp.BackupName) {
		t.Errorf("backup name shape: %q", resp.BackupName)
	}
	// The backup must live OUTSIDE custom.d/ so the include glob does not
	// auto-load it as a duplicate of the live override on the next reload.
	if _, err := os.Stat(filepath.Join(dir, resp.BackupName)); !os.IsNotExist(err) {
		t.Errorf("backup leaked into custom.d/, stat err=%v", err)
	}
	backup, err := os.ReadFile(filepath.Join(config.NginxCustomDBkp(), resp.BackupName))
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if string(backup) != "# original\n" {
		t.Errorf("backup content: %q", backup)
	}
	current, _ := os.ReadFile(filepath.Join(dir, "acme.test.conf"))
	if string(current) != "# new\n" {
		t.Errorf("current content: %q", current)
	}
}

func TestHandleSiteNginxBackups_listsNewestFirst(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	bkpDir := config.NginxCustomDBkp()
	if err := os.MkdirAll(bkpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(bkpDir, "acme.test.conf.bkp.20260101-101010")
	newer := filepath.Join(bkpDir, "acme.test.conf.bkp.20260601-120000")
	if err := os.WriteFile(older, []byte("# older\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("# newer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A backup for a different domain must not leak across the listing.
	if err := os.WriteFile(filepath.Join(bkpDir, "other.test.conf.bkp.20260601-120000"), []byte("# other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/nginx/backups", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var list []SiteNginxBackup
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("backup count: got %d want 2", len(list))
	}
	if list[0].Name != "acme.test.conf.bkp.20260601-120000" {
		t.Errorf("newest first: got %q", list[0].Name)
	}
}

func TestHandleSiteNginxBackupContent_returnsRawBytes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	bkpDir := config.NginxCustomDBkp()
	_ = os.MkdirAll(bkpDir, 0o755)
	name := "acme.test.conf.bkp.20260101-101010"
	if err := os.WriteFile(filepath.Join(bkpDir, name), []byte("# captured\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/nginx/backups/"+name, nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "# captured\n" {
		t.Errorf("body: %q", rec.Body.String())
	}
}

func TestHandleSiteNginxBackupContent_rejectsCrossDomainName(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	// A backup name that doesn't carry the acme.test prefix must 404 even if
	// the file exists on disk; otherwise the {name} segment would be a path
	// traversal vector across sites.
	req := httptest.NewRequest(http.MethodGet, "/api/sites/acme.test/nginx/backups/other.test.conf.bkp.20260101-101010", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d want 404", rec.Code)
	}
}

func TestHandleSiteNginxRestore_replacesAndDeletesBackup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	reloads := stubNginxReload(t)

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	dir := config.NginxCustomD()
	bkpDir := config.NginxCustomDBkp()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.MkdirAll(bkpDir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "acme.test.conf"), []byte("# current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupName := "acme.test.conf.bkp.20260601-120000"
	if err := os.WriteFile(filepath.Join(bkpDir, backupName), []byte("# from backup\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx/restore", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteNginxRestoreResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK || resp.Restored != backupName {
		t.Fatalf("response: %+v", resp)
	}
	if resp.Content != "# from backup\n" {
		t.Errorf("content: %q", resp.Content)
	}
	if *reloads != 1 {
		t.Errorf("reload calls: got %d want 1", *reloads)
	}
	if _, err := os.Stat(filepath.Join(bkpDir, backupName)); !os.IsNotExist(err) {
		t.Errorf("backup should be removed after restore, stat err=%v", err)
	}
	current, _ := os.ReadFile(filepath.Join(dir, "acme.test.conf"))
	if string(current) != "# from backup\n" {
		t.Errorf("current content: %q", current)
	}
}

func TestHandleSiteNginxRestore_noBackupReturnsError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubNginxReload(t)

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx/restore", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteNginxRestoreResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.OK || !strings.Contains(resp.Error, "no backup") {
		t.Errorf("want no-backup error, got %+v", resp)
	}
}

func TestHandleSiteNginxReset_removesFileAndReloads(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	reloads := stubNginxReload(t)

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	dir := config.NginxCustomD()
	_ = os.MkdirAll(dir, 0o755)
	confPath := filepath.Join(dir, "acme.test.conf")
	if err := os.WriteFile(confPath, []byte("# saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A pre-existing backup must survive reset so the user can recover.
	bkpDir := config.NginxCustomDBkp()
	_ = os.MkdirAll(bkpDir, 0o755)
	bkpName := "acme.test.conf.bkp.20260101-101010"
	if err := os.WriteFile(filepath.Join(bkpDir, bkpName), []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx/reset", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteNginxResetResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK {
		t.Errorf("want ok=true, got %+v", resp)
	}
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Errorf("file should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(bkpDir, bkpName)); err != nil {
		t.Errorf("backup must survive reset: %v", err)
	}
	if *reloads != 1 {
		t.Errorf("reload calls: got %d want 1", *reloads)
	}
}

func TestHandleSiteNginxReset_noOpWhenMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubNginxReload(t)

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx/reset", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteNginxResetResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.OK {
		t.Errorf("want ok=true even when file absent, got %+v", resp)
	}
}

// A failed `nginx -t` must restore the prior bytes verbatim and refuse to
// reload, so a typo in the editor cannot break the live nginx config.
func TestHandleSiteNginx_failedValidationRollsBackToPriorContent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	reloads := stubNginxReload(t)
	stubNginxTest(t, "nginx: [emerg] unknown directive \"oops\" in /etc/nginx/custom.d/acme.test.conf:1\nnginx: configuration file /etc/nginx/nginx.conf test failed", fmt.Errorf("exit status 1"))

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	dir := config.NginxCustomD()
	_ = os.MkdirAll(dir, 0o755)
	confPath := filepath.Join(dir, "acme.test.conf")
	if err := os.WriteFile(confPath, []byte("# original good\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(SiteNginxWriteRequest{Content: "oops broken;\n", Backup: true})
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteNginxWriteResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.OK {
		t.Fatalf("expected ok=false after validation failure, got %+v", resp)
	}
	if !strings.Contains(resp.Error, "rolled back") {
		t.Errorf("expected rolled-back error, got %q", resp.Error)
	}
	// The user-facing error must NOT carry os/exec exit-code noise; the
	// real nginx diagnostic belongs in ValidationOutput so it can be
	// rendered as a dedicated block in the modal.
	if strings.Contains(resp.Error, "exit status") {
		t.Errorf("error leaked exec exit-status noise: %q", resp.Error)
	}
	if !strings.Contains(resp.ValidationOutput, "unknown directive") {
		t.Errorf("expected nginx -t output in response, got %q", resp.ValidationOutput)
	}
	// Reload must NOT have been invoked when validation failed; the whole
	// point of the pre-flight is to avoid touching the running config.
	if *reloads != 0 {
		t.Errorf("reload should not run on validation failure, got %d calls", *reloads)
	}
	current, _ := os.ReadFile(confPath)
	if string(current) != "# original good\n" {
		t.Errorf("file should be rolled back, got %q", current)
	}
	// A failed save must not leave a stale backup behind, since the backup's
	// only purpose was to protect the (non-)transition that never happened.
	entries, _ := os.ReadDir(config.NginxCustomDBkp())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "acme.test.conf.bkp.") {
			t.Errorf("stale backup left after failed save: %s", e.Name())
		}
	}
}

// When the file did not exist before the save and validation fails, the
// rollback path must remove the file (not restore zero bytes), so the
// editor returns to its initial empty state rather than persisting an
// invalid stub.
func TestHandleSiteNginx_failedValidationRemovesNewFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubNginxReload(t)
	// Validation output must mention the file we wrote so the handler
	// owns the failure and rolls back; an unrelated -t failure should
	// fall through to the reload path (covered in another test).
	stubNginxTest(t, "[emerg] unknown directive in acme.test.conf:1\ntest failed", fmt.Errorf("exit 1"))

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(SiteNginxWriteRequest{Content: "oops;\n"})
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	confPath := filepath.Join(config.NginxCustomD(), "acme.test.conf")
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Errorf("expected file removed on rollback, stat err=%v", err)
	}
}

// nginx -t can fail because of a pre-existing broken neighbour vhost; in
// that case our save did not cause the failure and rolling back our own
// (perfectly valid) write would be a confusing false positive. The
// handler should only rollback when the -t output names OUR file.
func TestHandleSiteNginx_validationFailureFromNeighbourKeepsOurWrite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	reloads := stubNginxReload(t)
	stubNginxTest(t, "[emerg] unknown directive in other.test.conf:5\ntest failed", fmt.Errorf("exit 1"))

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(SiteNginxWriteRequest{Content: "client_max_body_size 100m;\n"})
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	confPath := filepath.Join(config.NginxCustomD(), "acme.test.conf")
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("our file should remain on disk: %v", err)
	}
	if string(data) != "client_max_body_size 100m;\n" {
		t.Errorf("content: %q", data)
	}
	// Reload is invoked even though -t mentioned a neighbour; the reload
	// fails are handled by the existing reload-failure path, but the
	// stub here returns nil so this case exercises the success path.
	if *reloads != 1 {
		t.Errorf("reload calls: got %d want 1", *reloads)
	}
}

// The save handler must place its temp file OUTSIDE custom.d/ so the
// vhost include glob {domain}.conf* cannot pick up a half-written file
// during a concurrent reload.
func TestHandleSiteNginx_tempFileStagesOutsideIncludeGlob(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	stubNginxReload(t)
	stubNginxTest(t, "ok", nil)

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(SiteNginxWriteRequest{Content: "x;\n"})
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	entries, err := os.ReadDir(config.NginxCustomD())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		// After a successful save the only file matching the include
		// glob must be the final {domain}.conf — any leftover .tmp.*
		// would have been a window where nginx could see a half-write.
		if strings.HasPrefix(name, "acme.test.conf.tmp") {
			t.Errorf("temp file leaked into custom.d/: %s", name)
		}
	}
}

func TestHandleSiteNginxRestore_namedBackup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	reloads := stubNginxReload(t)

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	dir := config.NginxCustomD()
	bkpDir := config.NginxCustomDBkp()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.MkdirAll(bkpDir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "acme.test.conf"), []byte("# current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	older := "acme.test.conf.bkp.20260101-101010"
	newer := "acme.test.conf.bkp.20260601-120000"
	if err := os.WriteFile(filepath.Join(bkpDir, older), []byte("# older\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bkpDir, newer), []byte("# newer\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Caller asks specifically for the older backup; server must restore
	// that exact one, not the default-newest.
	body, _ := json.Marshal(SiteNginxRestoreRequest{Name: older})
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp SiteNginxRestoreResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Restored != older {
		t.Errorf("restored: got %q want %q", resp.Restored, older)
	}
	current, _ := os.ReadFile(filepath.Join(dir, "acme.test.conf"))
	if string(current) != "# older\n" {
		t.Errorf("current content: %q", current)
	}
	// The newer backup must survive — only the restored one was consumed.
	if _, err := os.Stat(filepath.Join(bkpDir, newer)); err != nil {
		t.Errorf("non-restored backup must remain: %v", err)
	}
	if *reloads != 1 {
		t.Errorf("reload calls: got %d want 1", *reloads)
	}
}

// A restore that fails the post-swap reload must leave the backup in
// place so the user can retry, instead of dropping the recovery copy
// before confirming the reload landed.
func TestHandleSiteNginxRestore_reloadFailurePreservesBackup(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	prev := siteops.NginxReloadFn
	siteops.NginxReloadFn = func() error { return fmt.Errorf("podman exec failed") }
	t.Cleanup(func() { siteops.NginxReloadFn = prev })

	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	dir := config.NginxCustomD()
	bkpDir := config.NginxCustomDBkp()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.MkdirAll(bkpDir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "acme.test.conf"), []byte("# current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "acme.test.conf.bkp.20260101-101010"
	if err := os.WriteFile(filepath.Join(bkpDir, name), []byte("# backup\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/nginx/restore", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)

	var resp SiteNginxRestoreResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.OK {
		t.Errorf("expected ok=false when reload failed, got %+v", resp)
	}
	// Backup must NOT have been removed; the user needs it to retry.
	if _, err := os.Stat(filepath.Join(bkpDir, name)); err != nil {
		t.Errorf("backup must survive reload failure: %v", err)
	}
}
