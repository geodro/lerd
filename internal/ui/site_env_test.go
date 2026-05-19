package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// POST to /env is rejected so the GET-only contract isn't accidentally
// extended by future routes that share the dispatcher.
func TestHandleSiteEnv_nonGetRejected(t *testing.T) {
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
