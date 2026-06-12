package grouping

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
)

// setup points the registry at a temp dir and stubs the heavy regeneration so
// tests assert on registry state alone. It returns nothing; cleanup is handled
// by t via Setenv/TempDir.
func setup(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	orig := regenerateSecondary
	regenerateSecondary = func(_ *config.Site, _ string) error { return nil }
	t.Cleanup(func() { regenerateSecondary = orig })
}

func mustAdd(t *testing.T, s config.Site) {
	t.Helper()
	if err := config.AddSite(s); err != nil {
		t.Fatalf("AddSite(%s): %v", s.Name, err)
	}
}

func reload(t *testing.T, name string) *config.Site {
	t.Helper()
	s, err := config.FindSite(name)
	if err != nil {
		t.Fatalf("FindSite(%s): %v", name, err)
	}
	return s
}

// fakeMainRepo creates a .git directory so DetectWorktrees treats path as a
// main repo, optionally adding a worktree on the given branch.
func fakeMainRepo(t *testing.T, branch, checkout string) string {
	t.Helper()
	dir := t.TempDir()
	if branch == "" {
		os.MkdirAll(filepath.Join(dir, ".git"), 0755)
		return dir
	}
	wt := filepath.Join(dir, ".git", "worktrees", branch)
	if err := os.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(wt, "HEAD"), []byte("ref: refs/heads/"+branch+"\n"), 0644)
	if checkout == "" {
		checkout = t.TempDir()
	}
	os.MkdirAll(checkout, 0755)
	os.WriteFile(filepath.Join(wt, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0644)
	return dir
}

// ── ComputeSecondaryDomain ───────────────────────────────────────────────────

func TestComputeSecondaryDomain(t *testing.T) {
	if got := ComputeSecondaryDomain("starlane.test", "admin"); got != "admin.starlane.test" {
		t.Errorf("got %q, want admin.starlane.test", got)
	}
}

// ── ValidateLabel ────────────────────────────────────────────────────────────

func TestValidateLabel(t *testing.T) {
	cases := []struct {
		label string
		ok    bool
	}{
		{"admin", true},
		{"api-v2", true},
		{"", false},
		{"Admin", false},   // not canonical lowercase
		{"foo.bar", false}, // dot not allowed in a single label
		{"under_score", false},
		{"trailing-", false},
	}
	for _, c := range cases {
		err := ValidateLabel(c.label)
		if c.ok && err != nil {
			t.Errorf("ValidateLabel(%q) = %v, want nil", c.label, err)
		}
		if !c.ok && err == nil {
			t.Errorf("ValidateLabel(%q) = nil, want error", c.label)
		}
	}
}

// ── AssignSecondary happy path ───────────────────────────────────────────────

func TestAssignSecondary_groupsExistingSite(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	mustAdd(t, config.Site{Name: "admin-starlane", Domains: []string{"admin-starlane.test"}, Path: "/srv/admin"})

	main := reload(t, "starlane")
	sec := reload(t, "admin-starlane")
	if err := AssignSecondary(main, sec, "admin", false); err != nil {
		t.Fatalf("AssignSecondary: %v", err)
	}

	gotMain := reload(t, "starlane")
	if !gotMain.IsGroupMain() || gotMain.Group != "starlane" {
		t.Errorf("main not promoted: group=%q subdomain=%q", gotMain.Group, gotMain.GroupSubdomain)
	}
	gotSec := reload(t, "admin-starlane")
	if !gotSec.IsGroupSecondary() {
		t.Errorf("secondary not grouped: group=%q subdomain=%q", gotSec.Group, gotSec.GroupSubdomain)
	}
	if gotSec.PrimaryDomain() != "admin.starlane.test" {
		t.Errorf("secondary domain = %q, want admin.starlane.test", gotSec.PrimaryDomain())
	}
	if len(gotSec.Domains) != 1 {
		t.Errorf("old standalone domain not replaced: %v", gotSec.Domains)
	}
}

func TestAssignSecondary_rollsBackOnRegenFailure(t *testing.T) {
	setup(t)
	regenerateSecondary = func(_ *config.Site, _ string) error { return fmt.Errorf("boom") }
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	mustAdd(t, config.Site{Name: "admin", Domains: []string{"admin.test"}, Path: "/srv/admin"})

	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin"), "admin", false); err == nil {
		t.Fatal("expected regeneration failure to surface")
	}
	sec := reload(t, "admin")
	if sec.IsGroupSecondary() {
		t.Errorf("secondary not rolled back: group=%q subdomain=%q", sec.Group, sec.GroupSubdomain)
	}
	if sec.PrimaryDomain() != "admin.test" {
		t.Errorf("secondary domain not restored: %q", sec.PrimaryDomain())
	}
	if reload(t, "starlane").Group != "" {
		t.Error("main not demoted after rollback")
	}
}

// ── AssignSecondary rejections ───────────────────────────────────────────────

func TestAssignSecondary_rejectsSelf(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	s := reload(t, "starlane")
	if err := AssignSecondary(s, s, "admin", false); err == nil {
		t.Error("expected error grouping a site under itself")
	}
}

func TestAssignSecondary_rejectsInvalidLabel(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	mustAdd(t, config.Site{Name: "admin", Domains: []string{"admin.test"}, Path: "/srv/admin"})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin"), "Bad_Label", false); err == nil {
		t.Error("expected error for invalid label")
	}
}

func TestAssignSecondary_rejectsDomainInUse(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	mustAdd(t, config.Site{Name: "admin", Domains: []string{"admin.test"}, Path: "/srv/admin"})
	// A third site already squats the computed subdomain.
	mustAdd(t, config.Site{Name: "squatter", Domains: []string{"admin.starlane.test"}, Path: "/srv/squatter"})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin"), "admin", false); err == nil {
		t.Error("expected error when computed subdomain is already used")
	}
}

func TestAssignSecondary_rejectsSiblingLabelDup(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane", Group: "starlane"})
	mustAdd(t, config.Site{Name: "admin1", Domains: []string{"admin.starlane.test"}, Path: "/srv/admin1", Group: "starlane", GroupSubdomain: "admin"})
	mustAdd(t, config.Site{Name: "admin2", Domains: []string{"admin2.test"}, Path: "/srv/admin2"})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin2"), "admin", false); err == nil {
		t.Error("expected error for duplicate sibling label")
	}
}

func TestAssignSecondary_rejectsWorktreeLabelCollision(t *testing.T) {
	setup(t)
	mainPath := fakeMainRepo(t, "admin", "")
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: mainPath})
	mustAdd(t, config.Site{Name: "admin", Domains: []string{"admin.test"}, Path: "/srv/admin"})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin"), "admin", false); err == nil {
		t.Error("expected error when a main-site worktree already uses the label")
	}
}

func TestAssignSecondary_rejectsMainThatIsSecondary(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "sub", Domains: []string{"sub.starlane.test"}, Path: "/srv/sub", Group: "starlane", GroupSubdomain: "sub"})
	mustAdd(t, config.Site{Name: "other", Domains: []string{"other.test"}, Path: "/srv/other"})
	if err := AssignSecondary(reload(t, "sub"), reload(t, "other"), "x", false); err == nil {
		t.Error("expected error using a secondary as a group main")
	}
}

func TestAssignSecondary_rejectsSecondaryWithOwnSecondaries(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	mustAdd(t, config.Site{Name: "other", Domains: []string{"other.test"}, Path: "/srv/other", Group: "other"})
	mustAdd(t, config.Site{Name: "othersub", Domains: []string{"sub.other.test"}, Path: "/srv/othersub", Group: "other", GroupSubdomain: "sub"})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "other"), "x", false); err == nil {
		t.Error("expected error: groups are only one level deep")
	}
}

// ── UnassignSecondary ────────────────────────────────────────────────────────

func TestUnassignSecondary_restoresStandalone(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	mustAdd(t, config.Site{Name: "admin-starlane", Domains: []string{"admin-starlane.test"}, Path: "/srv/admin-starlane"})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin-starlane"), "admin", false); err != nil {
		t.Fatal(err)
	}

	if err := UnassignSecondary(reload(t, "admin-starlane")); err != nil {
		t.Fatalf("UnassignSecondary: %v", err)
	}
	sec := reload(t, "admin-starlane")
	if sec.IsGroupSecondary() {
		t.Errorf("still grouped: group=%q subdomain=%q", sec.Group, sec.GroupSubdomain)
	}
	if sec.PrimaryDomain() != "admin-starlane.test" {
		t.Errorf("standalone domain = %q, want admin-starlane.test", sec.PrimaryDomain())
	}
	// Last secondary gone -> main's group key cleared.
	if reload(t, "starlane").Group != "" {
		t.Errorf("main group key not cleared after last secondary removed")
	}
}

func TestUnassignSecondary_rejectsNonSecondary(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	if err := UnassignSecondary(reload(t, "starlane")); err == nil {
		t.Error("expected error unassigning a non-secondary")
	}
}

// ── SetSecondaryLabel ────────────────────────────────────────────────────────

func TestSetSecondaryLabel_changesDomain(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	mustAdd(t, config.Site{Name: "admin", Domains: []string{"admin.test"}, Path: "/srv/admin"})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin"), "admin", false); err != nil {
		t.Fatal(err)
	}
	if err := SetSecondaryLabel(reload(t, "admin"), "backoffice"); err != nil {
		t.Fatalf("SetSecondaryLabel: %v", err)
	}
	sec := reload(t, "admin")
	if sec.GroupSubdomain != "backoffice" || sec.PrimaryDomain() != "backoffice.starlane.test" {
		t.Errorf("label not changed: subdomain=%q domain=%q", sec.GroupSubdomain, sec.PrimaryDomain())
	}
}

// ── CascadeMainDomainChange ──────────────────────────────────────────────────

func TestCascadeMainDomainChange_reflowsSecondaries(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	mustAdd(t, config.Site{Name: "admin", Domains: []string{"admin.test"}, Path: "/srv/admin"})
	mustAdd(t, config.Site{Name: "api", Domains: []string{"api.test"}, Path: "/srv/api"})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin"), "admin", false); err != nil {
		t.Fatal(err)
	}
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "api"), "api", false); err != nil {
		t.Fatal(err)
	}

	// Rename the main's base domain, then cascade.
	main := reload(t, "starlane")
	main.Domains = []string{"astro.test"}
	mustAdd(t, *main)
	if err := CascadeMainDomainChange(reload(t, "starlane")); err != nil {
		t.Fatalf("CascadeMainDomainChange: %v", err)
	}

	if got := reload(t, "admin").PrimaryDomain(); got != "admin.astro.test" {
		t.Errorf("admin domain = %q, want admin.astro.test", got)
	}
	if got := reload(t, "api").PrimaryDomain(); got != "api.astro.test" {
		t.Errorf("api domain = %q, want api.astro.test", got)
	}
}

// ── DissolveGroup ────────────────────────────────────────────────────────────

func TestDissolveGroup_ungroupsAll(t *testing.T) {
	setup(t)
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: "/srv/starlane"})
	mustAdd(t, config.Site{Name: "admin", Domains: []string{"admin.test"}, Path: "/srv/admin"})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin"), "admin", false); err != nil {
		t.Fatal(err)
	}
	if err := DissolveGroup("starlane"); err != nil {
		t.Fatalf("DissolveGroup: %v", err)
	}
	if reload(t, "admin").IsGroupSecondary() {
		t.Error("secondary still grouped after dissolve")
	}
	if reload(t, "starlane").Group != "" {
		t.Error("main still has group key after dissolve")
	}
}

// ── Shared database ──────────────────────────────────────────────────────────

// siteWithEnv creates a real directory with a .env containing the given
// DB_DATABASE so the shared-DB env rewrites have a file to act on.
func siteWithEnv(t *testing.T, db string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_NAME=x\nDB_DATABASE="+db+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func envDB(t *testing.T, dir string) string {
	t.Helper()
	return envfile.ReadKey(filepath.Join(dir, ".env"), "DB_DATABASE")
}

func TestAssignSecondary_sharedDB_pointsAtMainDB(t *testing.T) {
	setup(t)
	mainDir := siteWithEnv(t, "starlane")
	secDir := siteWithEnv(t, "admin_starlane")
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: mainDir})
	mustAdd(t, config.Site{Name: "admin-starlane", Domains: []string{"admin-starlane.test"}, Path: secDir})

	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin-starlane"), "admin", true); err != nil {
		t.Fatalf("AssignSecondary: %v", err)
	}
	if !reload(t, "admin-starlane").GroupSharedDB {
		t.Error("GroupSharedDB not persisted")
	}
	if got := envDB(t, secDir); got != "starlane" {
		t.Errorf("secondary DB_DATABASE = %q, want starlane", got)
	}
}

func TestSetSecondarySharedDB_toggle(t *testing.T) {
	setup(t)
	mainDir := siteWithEnv(t, "starlane")
	secDir := siteWithEnv(t, "admin_starlane")
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: mainDir})
	mustAdd(t, config.Site{Name: "admin-starlane", Domains: []string{"admin-starlane.test"}, Path: secDir})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin-starlane"), "admin", false); err != nil {
		t.Fatal(err)
	}
	// Independent by default.
	if got := envDB(t, secDir); got != "admin_starlane" {
		t.Errorf("default DB_DATABASE = %q, want admin_starlane", got)
	}
	// Turn sharing on.
	if err := SetSecondarySharedDB(reload(t, "admin-starlane"), true); err != nil {
		t.Fatalf("SetSecondarySharedDB on: %v", err)
	}
	if got := envDB(t, secDir); got != "starlane" {
		t.Errorf("after share DB_DATABASE = %q, want starlane", got)
	}
	// Turn it back off — restores the secondary's own slug.
	if err := SetSecondarySharedDB(reload(t, "admin-starlane"), false); err != nil {
		t.Fatalf("SetSecondarySharedDB off: %v", err)
	}
	if got := envDB(t, secDir); got != "admin_starlane" {
		t.Errorf("after unshare DB_DATABASE = %q, want admin_starlane", got)
	}
}

func TestSharedDBNameFor(t *testing.T) {
	setup(t)
	mainDir := siteWithEnv(t, "starlane")
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: mainDir, Group: "starlane"})
	shared := config.Site{Name: "admin", Domains: []string{"admin.starlane.test"}, Path: "/srv/admin", Group: "starlane", GroupSubdomain: "admin", GroupSharedDB: true}
	mustAdd(t, shared)
	if name, ok := SharedDBNameFor(&shared); !ok || name != "starlane" {
		t.Errorf("SharedDBNameFor = %q,%v, want starlane,true", name, ok)
	}
	// A non-sharing secondary returns false.
	indep := config.Site{Name: "i", Domains: []string{"i.starlane.test"}, Path: "/srv/i", Group: "starlane", GroupSubdomain: "i"}
	if _, ok := SharedDBNameFor(&indep); ok {
		t.Error("expected SharedDBNameFor=false for non-sharing secondary")
	}
}

func TestUnassignSecondary_sharedDB_restoresOwnDB(t *testing.T) {
	setup(t)
	mainDir := siteWithEnv(t, "starlane")
	secDir := siteWithEnv(t, "admin_starlane")
	mustAdd(t, config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: mainDir})
	mustAdd(t, config.Site{Name: "admin-starlane", Domains: []string{"admin-starlane.test"}, Path: secDir})
	if err := AssignSecondary(reload(t, "starlane"), reload(t, "admin-starlane"), "admin", true); err != nil {
		t.Fatal(err)
	}
	if err := UnassignSecondary(reload(t, "admin-starlane")); err != nil {
		t.Fatalf("UnassignSecondary: %v", err)
	}
	if got := envDB(t, secDir); got != "admin_starlane" {
		t.Errorf("after ungroup DB_DATABASE = %q, want admin_starlane", got)
	}
}

// ── .lerd.yaml domain sync ───────────────────────────────────────────────────

func TestSyncSecondaryProjectDomains_replacesOldStandalone(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"),
		[]byte("php_version: \"8.4\"\ndomains:\n  - admin-starlane\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sec := &config.Site{Name: "admin-starlane", Domains: []string{"admin.starlane.test"}, Path: dir}

	syncSecondaryProjectDomains(sec, "admin-starlane.test")

	cfg, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Domains) != 1 || cfg.Domains[0] != "admin.starlane" {
		t.Errorf(".lerd.yaml domains = %v, want [admin.starlane] (old standalone dropped)", cfg.Domains)
	}
}

// ── WorktreeLabelTaken ───────────────────────────────────────────────────────

func TestWorktreeLabelTaken(t *testing.T) {
	setup(t)
	mainPath := fakeMainRepo(t, "admin", "")
	main := &config.Site{Name: "starlane", Domains: []string{"starlane.test"}, Path: mainPath}
	if taken, err := WorktreeLabelTaken(main, "admin"); err != nil || !taken {
		t.Errorf("expected admin label taken, got taken=%v err=%v", taken, err)
	}
	if taken, err := WorktreeLabelTaken(main, "free"); err != nil || taken {
		t.Errorf("expected free label available, got taken=%v err=%v", taken, err)
	}
}
