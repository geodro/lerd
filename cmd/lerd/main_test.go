package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
)

func isolateConfig(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))
	for _, d := range []string{
		config.ConfigDir(),
		config.DataDir(),
		config.NginxConfD(),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
}

func TestRemoveStale_removesDeletedNonParkedSite(t *testing.T) {
	isolateConfig(t)

	liveDir := t.TempDir()
	deletedDir := filepath.Join(t.TempDir(), "ghost")

	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "live", Domains: []string{"live.test"}, Path: liveDir},
		{Name: "ghost", Domains: []string{"ghost.test"}, Path: deletedDir},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	if !removeStale(&config.GlobalConfig{}) {
		t.Fatal("expected removeStale to report a removal")
	}

	after, err := config.LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(after.Sites))
	for _, s := range after.Sites {
		names = append(names, s.Name)
	}
	if len(names) != 1 || names[0] != "live" {
		t.Errorf("expected only [live] after sweep, got %v", names)
	}
}

func TestRemoveStale_keepsLiveSite(t *testing.T) {
	isolateConfig(t)

	liveDir := t.TempDir()
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "live", Domains: []string{"live.test"}, Path: liveDir},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	if removeStale(&config.GlobalConfig{}) {
		t.Errorf("expected no removals when all site dirs exist")
	}
	after, _ := config.LoadSites()
	if len(after.Sites) != 1 {
		t.Errorf("expected live site preserved, got %d sites", len(after.Sites))
	}
}

func TestRemoveStale_skipsIgnoredSites(t *testing.T) {
	isolateConfig(t)

	// Ignored site with a deleted path should NOT be touched — the user has
	// intentionally parked it in the "ignored" state and the sweep shouldn't
	// reap it out from under them.
	reg := &config.SiteRegistry{Sites: []config.Site{
		{Name: "archived", Domains: []string{"archived.test"}, Path: "/var/empty/does-not-exist", Ignored: true},
	}}
	if err := config.SaveSites(reg); err != nil {
		t.Fatal(err)
	}

	if removeStale(&config.GlobalConfig{}) {
		t.Errorf("removeStale should not touch ignored sites")
	}
	after, _ := config.LoadSites()
	if len(after.Sites) != 1 {
		t.Errorf("ignored site should be preserved, got %d sites", len(after.Sites))
	}
}

// cleanupWorktreeVhosts runs after a worktree is removed and re-generates
// vhosts for surviving worktrees. It must NOT touch the surviving worktree's
// .env or kick off EnsureWorktreeDeps (which would trigger composer install
// and the JS install on every survivor on every removal).
func TestCleanupWorktreeVhosts_doesNotTouchSurvivorEnv(t *testing.T) {
	isolateConfig(t)

	mainSite := filepath.Join(t.TempDir(), "myapp")
	survivor := filepath.Join(t.TempDir(), "myapp-feat")
	for _, d := range []string{
		filepath.Join(mainSite, ".git", "worktrees", "feat"),
		survivor,
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(mainSite, ".env"), []byte("APP_URL=http://myapp.test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wtMeta := filepath.Join(mainSite, ".git", "worktrees", "feat")
	if err := os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(survivor, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	site := &config.Site{
		Name:       "myapp",
		Domains:    []string{"myapp.test"},
		Path:       mainSite,
		PHPVersion: "8.3",
	}

	cleanupWorktreeVhosts(site)

	if _, err := os.Stat(filepath.Join(survivor, ".env")); err == nil {
		t.Fatal("cleanupWorktreeVhosts copied .env into survivor — EnsureWorktreeDeps must not run from cleanup path")
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected stat error on survivor .env: %v", err)
	}
}

// A removed worktree must lose its custom nginx override and backups, while a
// surviving worktree keeps its own.
func TestCleanupWorktreeVhosts_prunesRemovedWorktreeOverride(t *testing.T) {
	isolateConfig(t)

	mainSite := filepath.Join(t.TempDir(), "myapp")
	survivor := filepath.Join(t.TempDir(), "myapp-feat")
	for _, d := range []string{filepath.Join(mainSite, ".git", "worktrees", "feat"), survivor} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	wtMeta := filepath.Join(mainSite, ".git", "worktrees", "feat")
	if err := os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(survivor, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Both worktree vhosts exist in conf.d, but only "feat" still has a
	// worktree on disk; "gone" was removed.
	confD := config.NginxConfD()
	if err := os.MkdirAll(confD, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"feat.myapp.test", "gone.myapp.test"} {
		if err := os.WriteFile(filepath.Join(confD, d+".conf"), []byte("server {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	customD := config.NginxCustomD()
	bkpD := config.NginxCustomDBkp()
	for _, d := range []string{customD, bkpD} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	survivorOverride := filepath.Join(customD, "feat.myapp.test.conf")
	goneOverride := filepath.Join(customD, "gone.myapp.test.conf")
	goneBackup := filepath.Join(bkpD, "gone.myapp.test.conf.bkp.20260101-101010")
	for _, p := range []string{survivorOverride, goneOverride, goneBackup} {
		if err := os.WriteFile(p, []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	site := &config.Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: mainSite, PHPVersion: "8.3"}
	cleanupWorktreeVhosts(site)

	if _, err := os.Stat(goneOverride); !os.IsNotExist(err) {
		t.Error("removed worktree's override should be pruned")
	}
	if _, err := os.Stat(goneBackup); !os.IsNotExist(err) {
		t.Error("removed worktree's backup should be pruned")
	}
	if _, err := os.Stat(survivorOverride); err != nil {
		t.Errorf("surviving worktree's override must be kept: %v", err)
	}
}

// HEAD writes (commit, checkout, rebase, rename) fire "changed" via
// fsnotify. Resurrecting host workers on every HEAD write resurrected
// user stops on every commit — issue #375 (Bruno's Vite).
// cleanupWorktreeVhosts must not delete the custom override of a separately
// registered site whose primary is a subdomain of the cleaned site.
func TestCleanupWorktreeVhosts_keepsSiblingSubdomainSiteOverride(t *testing.T) {
	isolateConfig(t)

	mainSite := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(filepath.Join(mainSite, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "app", Domains: []string{"app.test"}, Path: mainSite, PHPVersion: "8.3"}); err != nil {
		t.Fatal(err)
	}
	// A real second site whose primary is admin.app.test.
	if err := config.AddSite(config.Site{Name: "admin", Domains: []string{"admin.app.test"}, Path: t.TempDir(), PHPVersion: "8.3"}); err != nil {
		t.Fatal(err)
	}

	confD := config.NginxConfD()
	customD := config.NginxCustomD()
	for _, d := range []string{confD, customD} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// admin.app.test has a vhost (matches app's suffix scan) and an override.
	if err := os.WriteFile(filepath.Join(confD, "admin.app.test.conf"), []byte("server {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	siblingOverride := filepath.Join(customD, "admin.app.test.conf")
	if err := os.WriteFile(siblingOverride, []byte("# sibling\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanupWorktreeVhosts(&config.Site{Name: "app", Domains: []string{"app.test"}, Path: mainSite, PHPVersion: "8.3"})

	if _, err := os.Stat(siblingOverride); err != nil {
		t.Errorf("sibling site's override must survive cleanup of app: %v", err)
	}
}

// Inheritance of the main override must fire only on genuine creation; on a
// "changed" event (commit/checkout) re-seeding would resurrect an override the
// user deliberately reset.
func TestShouldInheritNginxOnSync(t *testing.T) {
	cases := map[string]bool{"added": true, "changed": false, "": false, "removed": false}
	for action, want := range cases {
		if got := shouldInheritNginxOnSync(action); got != want {
			t.Errorf("shouldInheritNginxOnSync(%q) = %v, want %v", action, got, want)
		}
	}
}

func TestShouldAutoStartWorkersOnSync(t *testing.T) {
	cases := map[string]bool{
		"added":   true,
		"changed": false,
		"":        false,
		"removed": false,
	}
	for action, want := range cases {
		if got := shouldAutoStartWorkersOnSync(action); got != want {
			t.Errorf("shouldAutoStartWorkersOnSync(%q) = %v, want %v", action, got, want)
		}
	}
}

func TestPrintDNSDiagnostic_WarnStepShowsHint(t *testing.T) {
	// Regression for the field-report scenario: the legacy-host-resolver
	// path emits a WARN step whose Hint explains how to switch back to
	// lerd-managed DNS. The renderer used to print hints only on FAIL
	// steps, silently swallowing this guidance.
	diag := dns.Diagnostic{
		TLD:          "test",
		FirstFailure: -1,
		Steps: []dns.Step{{
			Name:   "lerd-dns container",
			Status: dns.StepWarn,
			Detail: "not running; a host-side resolver on :5300 is answering .test with 127.0.0.1",
			Hint:   "lerd is not managing DNS here. Stop your host resolver and run `lerd start` to switch.",
		}},
	}
	var buf bytes.Buffer
	printDNSDiagnostic(&buf, diag)
	out := buf.String()

	for _, want := range []string{
		"DNS is working",
		"! lerd-dns container",
		"host-side resolver on :5300",
		"hint:",
		"Stop your host resolver",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestPrintDNSDiagnostic_FailStepShowsHint(t *testing.T) {
	diag := dns.Diagnostic{
		TLD:          "test",
		FirstFailure: 0,
		Steps: []dns.Step{{
			Name:   "lerd-dns container",
			Status: dns.StepFail,
			Detail: "not running",
			Hint:   "lerd start  (or check podman logs lerd-dns)",
		}},
	}
	var buf bytes.Buffer
	printDNSDiagnostic(&buf, diag)
	out := buf.String()
	for _, want := range []string{"DNS is NOT working", "✗ lerd-dns container", "hint: lerd start"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestPrintDNSDiagnostic_OKStepHasNoHintLine(t *testing.T) {
	diag := dns.Diagnostic{
		TLD:          "test",
		FirstFailure: -1,
		Steps: []dns.Step{{
			Name:   "lerd-dns container",
			Status: dns.StepOK,
			Detail: "running",
			Hint:   "this hint should be ignored on OK steps",
		}},
	}
	var buf bytes.Buffer
	printDNSDiagnostic(&buf, diag)
	out := buf.String()
	if strings.Contains(out, "hint:") {
		t.Errorf("OK step should not print a hint line, got:\n%s", out)
	}
}
