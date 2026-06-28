package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteinfo"
)

// stubUnitStatus reports the units in active as running and everything else as
// stopped, so a test can pin the ground-truth worker state behind UnitStatus.
type stubUnitStatus struct{ active map[string]bool }

func (s stubUnitStatus) Start(string) error   { return nil }
func (s stubUnitStatus) Stop(string) error    { return nil }
func (s stubUnitStatus) Restart(string) error { return nil }
func (s stubUnitStatus) UnitStatus(name string) (string, error) {
	if s.active[name] {
		return "active", nil
	}
	return "inactive", nil
}
func (s stubUnitStatus) AllUnitStates() map[string]string { return nil }

// idleWorkerResumable must mirror resumeWorkerByName exactly: a worker the resume
// path can't bring back (an orphaned unit with no framework definition) must be
// reported non-resumable so idle-suspend never strands it stopped.
func TestIdleWorkerResumable(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "site")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	proj, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	proj.CustomWorkers = map[string]config.FrameworkWorker{
		"queue": {Command: "php artisan queue:work"},
	}
	proj.Proxy = &config.ProxyConfig{Command: "npm run dev", Port: 5173}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}

	site := &config.Site{Name: "site", Framework: "laravel", Path: dir}
	cases := map[string]bool{
		"queue":             true,  // framework worker
		"stripe":            true,  // handled explicitly by resumeWorkerByName
		hostProxyWorkerName: true,  // resumable while the project declares a proxy
		"some-orphan-unit":  false, // no framework definition -> not resumable
	}
	for name, want := range cases {
		if got := idleWorkerResumable(site, name); got != want {
			t.Errorf("idleWorkerResumable(%q) = %v, want %v", name, got, want)
		}
	}

	// With the proxy block removed, the host-proxy worker is no longer resumable
	// (resumeWorkerByName would no-op), so idle-suspend must not stop it.
	proj.Proxy = nil
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}
	if idleWorkerResumable(site, hostProxyWorkerName) {
		t.Error("host-proxy worker must be non-resumable when the project has no proxy block")
	}
}

// ClearIdleSuspendOnStart must drop the started worker from the site's persisted
// idle-suspended set so a later lerd-ui boot doesn't believe a now-running worker
// is still asleep and refuse to re-suspend it. This is the fix for workers staying
// up on an idle site after an install/relink started them.
func TestClearIdleSuspendOnStart_mainSite(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if err := config.AddSite(config.Site{
		Name: "myapp", Path: "/srv/myapp", Domains: []string{"myapp.test"},
		IdleSuspendedWorkers: []string{"queue", "schedule", "vite"},
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}

	ClearIdleSuspendOnStart("myapp", "/srv/myapp", "queue")

	site, err := config.FindSite("myapp")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	want := []string{"schedule", "vite"}
	if got := site.IdleSuspendedWorkers; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("idle_suspended_workers = %v, want %v", got, want)
	}
}

// Clearing the last suspended worker must leave an empty set (len 0) so the engine
// reconciles the site to not-suspended.
func TestClearIdleSuspendOnStart_lastWorkerEmptiesSet(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if err := config.AddSite(config.Site{
		Name: "myapp", Path: "/srv/myapp", Domains: []string{"myapp.test"},
		IdleSuspendedWorkers: []string{"queue"},
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}

	ClearIdleSuspendOnStart("myapp", "/srv/myapp", "queue")

	site, err := config.FindSite("myapp")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(site.IdleSuspendedWorkers) != 0 {
		t.Errorf("idle_suspended_workers = %v, want empty", site.IdleSuspendedWorkers)
	}
}

// Starting a worker that isn't in the suspended set must not touch the set (and is
// the cheap common case for the vast majority of starts).
func TestClearIdleSuspendOnStart_absentWorkerNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)

	if err := config.AddSite(config.Site{
		Name: "myapp", Path: "/srv/myapp", Domains: []string{"myapp.test"},
		IdleSuspendedWorkers: []string{"queue"},
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}

	ClearIdleSuspendOnStart("myapp", "/srv/myapp", "horizon")

	site, err := config.FindSite("myapp")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(site.IdleSuspendedWorkers) != 1 || site.IdleSuspendedWorkers[0] != "queue" {
		t.Errorf("idle_suspended_workers = %v, want [queue] untouched", site.IdleSuspendedWorkers)
	}
}

// A start in a worktree checkout (sitePath != site.Path) must reconcile that
// worktree's own slot, not the main site's.
func TestClearIdleSuspendOnStart_worktree(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)

	wtBase := config.WorktreeUnitSlug("feature-x")
	if err := config.AddSite(config.Site{
		Name: "myapp", Path: "/srv/myapp", Domains: []string{"myapp.test"},
		IdleSuspendedWorkers:  []string{"queue"},
		WorktreeIdleSuspended: map[string][]string{wtBase: {"vite"}},
	}); err != nil {
		t.Fatalf("seed site: %v", err)
	}

	ClearIdleSuspendOnStart("myapp", "/srv/myapp/feature-x", "vite")

	site, err := config.FindSite("myapp")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := site.WorktreeIdleSuspended[wtBase]; ok {
		t.Errorf("worktree slot should be cleared, got %v", site.WorktreeIdleSuspended)
	}
	if len(site.IdleSuspendedWorkers) != 1 || site.IdleSuspendedWorkers[0] != "queue" {
		t.Errorf("main-site set must be untouched, got %v", site.IdleSuspendedWorkers)
	}
}

// IdleSuspendStateIsStale must detect the drift that wedges the engine: a worker
// marked idle-suspended in the persisted list while its unit is actually running.
func TestIdleSuspendStateIsStale(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "site")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	proj, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	proj.CustomWorkers = map[string]config.FrameworkWorker{"queue": {Command: "php artisan queue:work"}}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}

	site := &config.Site{Name: "site", Framework: "laravel", Path: dir, IdleSuspendedWorkers: []string{"queue"}}
	t.Cleanup(func() { podman.UnitLifecycle = nil })

	// queue running while marked suspended -> stale.
	podman.UnitLifecycle = stubUnitStatus{active: map[string]bool{"lerd-queue-site": true}}
	if !IdleSuspendStateIsStale(site) {
		t.Error("expected stale: queue is running while marked suspended")
	}

	// queue genuinely stopped -> the claim matches reality, not stale.
	podman.UnitLifecycle = stubUnitStatus{active: map[string]bool{}}
	if IdleSuspendStateIsStale(site) {
		t.Error("expected not stale: queue is stopped, matching the suspended claim")
	}

	// An empty suspended set is never stale.
	site.IdleSuspendedWorkers = nil
	if IdleSuspendStateIsStale(site) {
		t.Error("empty suspended set is never stale")
	}
}

// appendLostSuspended must re-mark declared workers whose unit was removed
// entirely (idle-suspend's signature, so their marking was lost while asleep),
// while never touching a worker whose unit still exists (running, crashed, or
// merely stopped — left to worker-healing), a duplicate, an orphan with no
// framework definition, or a worker the user removed from .lerd.yaml.
func TestAppendLostSuspended(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "site")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	proj, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	proj.CustomWorkers = map[string]config.FrameworkWorker{
		"queue":   {Command: "php artisan queue:work"},
		"horizon": {Command: "php artisan horizon"},
	}
	proj.Workers = []string{"queue", "horizon", "stripe", "some-orphan"}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}
	site := &config.Site{Name: "site", Framework: "laravel", Path: dir}

	// present pins which units still exist (any state); a worker keyed here is
	// treated as not idle-suspended and must be left alone.
	present := map[string]string{}
	t.Cleanup(func() { unitStatesOKFn = siteinfo.AllUnitStatesOK })
	unitStatesOKFn = func() (map[string]string, bool) { return present, true }

	// All declared+resumable units removed -> all re-marked; the orphan is skipped.
	if got := appendLostSuspended(site, nil); !slices.Equal(got, []string{"queue", "horizon", "stripe"}) {
		t.Errorf("all-removed: got %v, want [queue horizon stripe]", got)
	}
	// A worker whose unit still exists (e.g. crashed/failed) is not re-marked, so
	// worker-healing still sees it instead of it being masked as sleeping.
	present = map[string]string{"lerd-queue-site": "failed"}
	if got := appendLostSuspended(site, nil); !slices.Equal(got, []string{"horizon", "stripe"}) {
		t.Errorf("queue unit present: got %v, want [horizon stripe]", got)
	}
	// An already-listed worker isn't duplicated; the rest are appended.
	present = map[string]string{}
	if got := appendLostSuspended(site, []string{"horizon"}); !slices.Equal(got, []string{"horizon", "queue", "stripe"}) {
		t.Errorf("horizon pre-listed: got %v, want [horizon queue stripe]", got)
	}

	// A worker the user removed is gone from .lerd.yaml, so it is never re-marked
	// even though its unit is absent like a sleepy one.
	proj.Workers = []string{"queue"}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}
	if got := appendLostSuspended(site, nil); !slices.Equal(got, []string{"queue"}) {
		t.Errorf("undeclared worker must not be re-marked: got %v, want [queue]", got)
	}
}

// TestAppendLostSuspended_ignoresUntrustedSnapshot: when the systemctl
// enumeration fails the snapshot is empty and untrustworthy. The detector must
// re-mark nothing — treating every declared worker as removed would later resume
// workers the user never had running (an opted-out vite/queue).
func TestAppendLostSuspended_ignoresUntrustedSnapshot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "site")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	proj, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	proj.CustomWorkers = map[string]config.FrameworkWorker{"queue": {Command: "php artisan queue:work"}}
	proj.Workers = []string{"queue", "stripe"}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}
	site := &config.Site{Name: "site", Framework: "laravel", Path: dir}

	t.Cleanup(func() { unitStatesOKFn = siteinfo.AllUnitStatesOK })
	// Empty map with ok=false models a failed enumeration: every unit looks absent.
	unitStatesOKFn = func() (map[string]string, bool) { return map[string]string{}, false }

	if got := appendLostSuspended(site, nil); len(got) != 0 {
		t.Errorf("failed enumeration must re-mark nothing, got %v", got)
	}
}

// SuspendWorkersForIdle must return declared workers whose unit was removed so an
// idle site whose persisted list was wiped (units already gone) is re-marked as
// sleepy rather than stranded showing "off". No worker is running here, so nothing
// is stopped; the result is purely the recovered set.
func TestSuspendWorkersForIdle_remarksAbsent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "site")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	proj, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	proj.CustomWorkers = map[string]config.FrameworkWorker{"queue": {Command: "php artisan queue:work"}}
	proj.Workers = []string{"queue"}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		podman.UnitLifecycle = nil
		unitStatesOKFn = siteinfo.AllUnitStatesOK
	})
	podman.UnitLifecycle = stubUnitStatus{active: map[string]bool{}}                       // nothing running
	unitStatesOKFn = func() (map[string]string, bool) { return map[string]string{}, true } // queue unit removed, enum OK

	site := &config.Site{Name: "site", Framework: "laravel", Path: dir}
	if got := SuspendWorkersForIdle(site); len(got) != 1 || got[0] != "queue" {
		t.Errorf("SuspendWorkersForIdle = %v, want [queue] (re-marked from .lerd.yaml)", got)
	}
}

func TestCompactDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m"},
		{41 * time.Minute, "41m"},
		{90 * time.Minute, "1h"},
		{25 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
	}
	for _, tc := range cases {
		if got := compactDuration(tc.d); got != tc.want {
			t.Errorf("compactDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
