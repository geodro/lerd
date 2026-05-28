package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSiteEnv writes a .env file at sitePath with the given body.
func writeSiteEnv(t *testing.T, sitePath, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(sitePath, ".env"), []byte(body), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
}

func readSiteEnv(t *testing.T, sitePath string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(sitePath, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	return string(body)
}

// Renaming the primary domain via the UI's domain:edit handler must rewrite
// APP_URL in the project's .env to reflect the new domain. Mirrors the
// envfile.SyncPrimaryDomain call the CLI domain handlers run after every
// primary-changing mutation; previously the UI did the registry/vhost/cert
// work but never touched .env, so APP_URL kept pointing at the old domain.
func TestHandleSiteAction_domainEdit_syncsAppURLOnPrimaryChange(t *testing.T) {
	sitePath := setupSecuredSite(t, "oldname")
	writeSiteEnv(t, sitePath, "APP_NAME=Demo\nAPP_URL=https://oldname.test\nAPP_ENV=local\n")

	req := httptest.NewRequest(http.MethodPost, "/api/sites/oldname.test/domain:edit?old=oldname&new=newname", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("expected ok response, got %s", rec.Body.String())
	}

	got := readSiteEnv(t, sitePath)
	if !strings.Contains(got, "APP_URL=https://newname.test") {
		t.Errorf("APP_URL not rewritten to new primary, got:\n%s", got)
	}
	if strings.Contains(got, "APP_URL=https://oldname.test") {
		t.Errorf("old APP_URL still present, got:\n%s", got)
	}
}

// Removing the primary domain must sync APP_URL to the next-in-line domain
// that becomes primary. Same regression shape as domain:edit.
func TestHandleSiteAction_domainRemove_syncsAppURLWhenPrimaryShifts(t *testing.T) {
	sitePath := setupSecuredSite(t, "primary", "secondary.test")
	writeSiteEnv(t, sitePath, "APP_URL=https://primary.test\n")

	req := httptest.NewRequest(http.MethodPost, "/api/sites/primary.test/domain:remove?name=primary", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("expected ok response, got %s", rec.Body.String())
	}

	got := readSiteEnv(t, sitePath)
	if !strings.Contains(got, "APP_URL=https://secondary.test") {
		t.Errorf("APP_URL not shifted to new primary, got:\n%s", got)
	}
}

// Worktree .env files must also get APP_URL rewritten to <branch>.<newPrimary>
// when the parent primary changes. Previously each worktree kept pointing at
// <branch>.<oldPrimary> until the user manually fixed it. Matches the
// migrate_tld behaviour for full-site renames.
func TestHandleSiteAction_domainEdit_syncsWorktreeAppURLOnPrimaryChange(t *testing.T) {
	sitePath := setupSecuredSite(t, "oldname")
	writeSiteEnv(t, sitePath, "APP_URL=https://oldname.test\n")

	wtPath := filepath.Join(t.TempDir(), "wt-feature")
	makeWorktree(t, sitePath, "feature", "feature", wtPath)
	writeSiteEnv(t, wtPath, "APP_URL=https://feature.oldname.test\n")

	req := httptest.NewRequest(http.MethodPost, "/api/sites/oldname.test/domain:edit?old=oldname&new=newname", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	got := readSiteEnv(t, wtPath)
	if !strings.Contains(got, "APP_URL=https://feature.newname.test") {
		t.Errorf("worktree APP_URL not rewritten to <branch>.<newPrimary>, got:\n%s", got)
	}
}

// Adding a non-primary domain must leave .env byte-identical, since the
// primary is untouched (new domains are appended). The test plants a
// VITE_REVERB_HOST that doesn't match the current primary so any erroneous
// call to SyncPrimaryDomain would rewrite it and the byte compare fails.
// Without that decoy a same-value SyncPrimaryDomain call would be invisible.
func TestHandleSiteAction_domainAdd_leavesEnvUntouchedWhenPrimaryUnchanged(t *testing.T) {
	sitePath := setupSecuredSite(t, "primary")
	original := "APP_URL=https://primary.test\nVITE_REVERB_HOST=stale.test\n"
	writeSiteEnv(t, sitePath, original)

	req := httptest.NewRequest(http.MethodPost, "/api/sites/primary.test/domain:add?name=alias", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	got := readSiteEnv(t, sitePath)
	if got != original {
		t.Errorf(".env was modified despite primary unchanged.\nwant:\n%s\n got:\n%s", original, got)
	}
}
