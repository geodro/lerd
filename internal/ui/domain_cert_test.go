package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
)

// installFakeMkcert writes a stub mkcert binary into the test's bin dir.
// The stub echoes every SAN it was asked for into the cert file body so
// callers can grep for them. Mirrors the pattern used by the certs
// package tests so it stays familiar.
func installFakeMkcert(t *testing.T, dataHome string) {
	t.Helper()
	binDir := filepath.Join(dataHome, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const script = `#!/bin/sh
CRT=""
KEY=""
SANS=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -cert-file) shift; CRT="$1" ;;
    -key-file)  shift; KEY="$1" ;;
    *) SANS="$SANS $1" ;;
  esac
  shift
done
printf '%s' "$SANS" > "$CRT"
printf 'KEY' > "$KEY"
`
	if err := os.WriteFile(filepath.Join(binDir, "mkcert"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// stubPodmanOnPath installs a no-op podman binary into stubDir and prepends
// stubDir to PATH. Without it nginx.Reload would shell out to real podman
// and create container storage under the test's tmp XDG_DATA_HOME that
// the runner can't delete (permission denied on overlay diffs).
func stubPodmanOnPath(t *testing.T) {
	t.Helper()
	stubDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubDir, "podman"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// makeWorktree fakes a git worktree at <sitePath>/.git/worktrees/<name>
// pointing at <checkoutPath>. Matches what real `git worktree add` writes
// so gitpkg.DetectWorktrees picks it up.
func makeWorktree(t *testing.T, sitePath, name, branch, checkoutPath string) {
	t.Helper()
	wtDir := filepath.Join(sitePath, ".git", "worktrees", name)
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(checkoutPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "gitdir"), []byte(filepath.Join(checkoutPath, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupSecuredSite primes XDG, fake mkcert, global config, and registers a
// secured site at sitePath with the given domains. Returns the site struct
// so the test can call config.AddSite again after mutating Domains, etc.
func setupSecuredSite(t *testing.T, primary string, extras ...string) (sitePath string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	stubPodmanOnPath(t)
	installFakeMkcert(t, tmp)

	cfg := &config.GlobalConfig{}
	cfg.DNS.Enabled = true
	cfg.DNS.TLD = "test"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	sitePath = filepath.Join(tmp, "project")
	if err := os.MkdirAll(sitePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sitePath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	domains := append([]string{primary + ".test"}, extras...)
	site := config.Site{
		Name:    strings.TrimSuffix(primary, ".test"),
		Path:    sitePath,
		Domains: domains,
		Secured: true,
	}
	if err := config.AddSite(site); err != nil {
		t.Fatalf("AddSite: %v", err)
	}
	return sitePath
}

// readSiteCert returns the raw bytes of <XDG_DATA_HOME>/lerd/certs/sites/<primary>.crt.
func readSiteCert(t *testing.T, primary string) string {
	t.Helper()
	path := filepath.Join(os.Getenv("XDG_DATA_HOME"), "lerd", "certs", "sites", primary+".crt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cert %q: %v", path, err)
	}
	return string(body)
}

// Adding a domain to a secured site must reissue the cert so the new SAN
// covers the new hostname AND so any existing worktree subdomains stay in
// the SAN list. Regression pin for two related bugs:
//
//  1. The handler used certs.IssueCert (a documented no-op when the cert
//     file already exists), so the new domain never made it into the SAN
//     and the browser rejected it with ERR_CERT_AUTHORITY_INVALID.
//  2. After switching to certs.IssueCertForce directly, the worktree
//     subdomain (<branch>.<primary>) silently dropped out of the SAN
//     because that bypass skipped the worktree-aware helper.
//
// Catching either regression requires asserting all three SAN classes
// here: new alias, existing primary, and worktree subdomain.
func TestHandleSiteAction_domainAdd_keepsWorktreeSANs(t *testing.T) {
	sitePath := setupSecuredSite(t, "theregistry")
	makeWorktree(t, sitePath, "main", "main", filepath.Join(t.TempDir(), "wt-main"))

	// Pre-issue the cert so it already exists on disk when domain:add
	// runs. Without this precondition a regression to bare certs.IssueCert
	// would still write a fresh cert with the new SAN (because no prior
	// file exists to short-circuit on) and the test would miss the bug.
	if err := certs.ReissueCertForWorktree(config.Site{
		Name: "theregistry", Path: sitePath,
		Domains: []string{"theregistry.test"}, Secured: true,
	}); err != nil {
		t.Fatalf("pre-issue cert: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/theregistry.test/domain:add?name=aaaddd", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("expected ok response, got %s", rec.Body.String())
	}

	cert := readSiteCert(t, "theregistry.test")
	for _, san := range []string{
		"theregistry.test", "*.theregistry.test",
		"aaaddd.test", "*.aaaddd.test",
		"main.theregistry.test", "*.main.theregistry.test",
	} {
		if !strings.Contains(cert, san) {
			t.Errorf("SAN %q missing after domain:add; cert body: %q", san, cert)
		}
	}
}

// Removing a domain must reissue the cert so the SAN list drops the
// removed alias but keeps the worktree subdomains. Same regression shape
// as domain:add — the original handler used the cert-skipping IssueCert,
// and the first attempted fix dropped worktree SANs. Pin both.
func TestHandleSiteAction_domainRemove_keepsWorktreeSANs(t *testing.T) {
	sitePath := setupSecuredSite(t, "theregistry", "aaaddd.test")
	makeWorktree(t, sitePath, "main", "main", filepath.Join(t.TempDir(), "wt-main"))

	if err := certs.ReissueCertForWorktree(config.Site{
		Name: "theregistry", Path: sitePath,
		Domains: []string{"theregistry.test", "aaaddd.test"}, Secured: true,
	}); err != nil {
		t.Fatalf("pre-issue cert: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sites/theregistry.test/domain:remove?name=aaaddd", nil)
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("expected ok response, got %s", rec.Body.String())
	}

	cert := readSiteCert(t, "theregistry.test")
	for _, san := range []string{
		"theregistry.test", "*.theregistry.test",
		"main.theregistry.test", "*.main.theregistry.test",
	} {
		if !strings.Contains(cert, san) {
			t.Errorf("SAN %q missing after domain:remove; cert body: %q", san, cert)
		}
	}
	for _, gone := range []string{"aaaddd.test", "*.aaaddd.test"} {
		// The fake mkcert echoes every arg, so the removed SAN must
		// not appear anywhere in the rewritten cert body.
		if strings.Contains(cert, gone) {
			t.Errorf("SAN %q should have been dropped after domain:remove; cert body: %q", gone, cert)
		}
	}
}
