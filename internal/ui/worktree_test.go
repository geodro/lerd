package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// handleSiteWorktreeOptions must return the documented JSON shape for a
// registered site, including build/db option lists and the migrate flag.
func TestHandleSiteWorktreeOptions(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sitePath := t.TempDir()
	if err := os.WriteFile(filepath.Join(sitePath, "artisan"), []byte("#!/usr/bin/env php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "acme", Path: sitePath, Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sites/worktree-options?domain=acme.test", nil)
	rec := httptest.NewRecorder()
	handleSiteWorktreeOptions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		BuildOptions  []labeledOption `json:"build_options"`
		BuildDefault  string          `json:"build_default"`
		DBOptions     []labeledOption `json:"db_options"`
		CanMigrate    bool            `json:"can_migrate"`
		LocalBranches []string        `json:"local_branches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if resp.BuildDefault != "auto" {
		t.Errorf("build_default: got %q want auto", resp.BuildDefault)
	}
	if len(resp.BuildOptions) == 0 || resp.BuildOptions[0].Value != "auto" {
		t.Errorf("build_options should start with auto, got %+v", resp.BuildOptions)
	}
	if last := resp.BuildOptions[len(resp.BuildOptions)-1]; last.Value != "skip" {
		t.Errorf("build_options should end with skip, got %+v", resp.BuildOptions)
	}
	if !resp.CanMigrate {
		t.Error("can_migrate should be true when artisan exists")
	}
	if !hasOption(resp.DBOptions, "share") || !hasOption(resp.DBOptions, "clone-main") {
		t.Errorf("db_options missing share/clone-main: %+v", resp.DBOptions)
	}
}

func TestHandleSiteWorktreeOptions_unknownSite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/sites/worktree-options?domain=nope.test", nil)
	rec := httptest.NewRecorder()
	handleSiteWorktreeOptions(rec, req)
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["error"] == nil {
		t.Fatalf("expected error for unknown site, got %s", rec.Body.String())
	}
}

// worktree:remove must reject a request with no branch parameter.
func TestHandleSiteAction_worktreeRemoveRequiresBranch(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/worktree:remove", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	var resp SiteActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error == "" {
		t.Fatalf("expected error when branch missing, got %s", rec.Body.String())
	}
}

func hasOption(opts []labeledOption, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}
