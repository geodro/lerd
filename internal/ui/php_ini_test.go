package ui

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestPhpIniScope checks the php.ini editor scope token resolves a bare version
// to the shared per-version file and "site:<name>" to a FrankenPHP site's own
// per-site file, so the same editor serves both.
func TestPhpIniScope(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if name, ok := phpIniSiteName("site:personal"); !ok || name != "personal" {
		t.Errorf("phpIniSiteName(site:personal) = (%q, %v), want (personal, true)", name, ok)
	}
	if _, ok := phpIniSiteName("8.5"); ok {
		t.Error("a bare version must not parse as a site scope")
	}

	verFile := phpIniScopeFile("8.5").Path
	if verFile != config.PHPUserIniFile("8.5") {
		t.Errorf("version scope path = %q, want the per-version file", verFile)
	}
	siteFile := phpIniScopeFile("site:personal").Path
	if siteFile != config.SitePHPUserIniFile("personal") {
		t.Errorf("site scope path = %q, want the per-site file", siteFile)
	}
	if verFile == siteFile {
		t.Error("version and site scopes must resolve to different files")
	}
	if !strings.Contains(siteFile, "/sites/personal/") {
		t.Errorf("site scope path %q should be under the per-site dir", siteFile)
	}
}
