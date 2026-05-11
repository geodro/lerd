package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// setupOptedInProject seeds .lerd.yaml with a host-mode custom worker named
// "vite" and the given workers opt-in list, returning the project dir. We use
// CustomWorkers so the worker definition lands in GetFrameworkForDir without
// touching the global framework store.
func setupOptedInProject(t *testing.T, optedIn []string) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
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
	tr := true
	proj.Workers = optedIn
	proj.CustomWorkers = map[string]config.FrameworkWorker{
		"vite":  {Command: "npm run dev", Host: true, PerWorktree: &tr, ReplacesBuild: true},
		"queue": {Command: "php artisan queue:work"},
	}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOptedInHostWorkers_optedIn(t *testing.T) {
	dir := setupOptedInProject(t, []string{"vite"})
	site := &config.Site{Name: "site", Path: dir, Framework: "laravel"}
	wt := filepath.Join(dir, "wt-main")
	if err := os.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}
	got := OptedInHostWorkers(site, wt)
	if len(got) != 1 || got[0] != "vite" {
		t.Errorf("got %v, want [vite]", got)
	}
}

func TestOptedInHostWorkers_notOptedIn(t *testing.T) {
	dir := setupOptedInProject(t, []string{"queue"})
	site := &config.Site{Name: "site", Path: dir, Framework: "laravel"}
	wt := filepath.Join(dir, "wt-main")
	if err := os.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}
	if got := OptedInHostWorkers(site, wt); len(got) != 0 {
		t.Errorf("queue is not host-mode and vite is not opted in; want [], got %v", got)
	}
}

func TestOptedInHostWorkers_emptyWorkers(t *testing.T) {
	dir := setupOptedInProject(t, nil)
	site := &config.Site{Name: "site", Path: dir, Framework: "laravel"}
	wt := filepath.Join(dir, "wt-main")
	if err := os.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}
	if got := OptedInHostWorkers(site, wt); len(got) != 0 {
		t.Errorf("no workers opted in; want [], got %v", got)
	}
}

func TestOptedInHostWorkers_skipsContainerWorkers(t *testing.T) {
	// queue is opted in but is not host:true. Auto-start belongs to other
	// code paths (queue:start, the systemd queue services); we should not
	// surface it here regardless.
	dir := setupOptedInProject(t, []string{"vite", "queue"})
	site := &config.Site{Name: "site", Path: dir, Framework: "laravel"}
	wt := filepath.Join(dir, "wt-main")
	if err := os.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}
	got := OptedInHostWorkers(site, wt)
	if len(got) != 1 || got[0] != "vite" {
		t.Errorf("got %v, want [vite] only", got)
	}
}

func TestOptedInHostWorkers_emptyFramework(t *testing.T) {
	dir := setupOptedInProject(t, []string{"vite"})
	site := &config.Site{Name: "site", Path: dir, Framework: ""}
	if got := OptedInHostWorkers(site, dir); len(got) != 0 {
		t.Errorf("no framework, want [], got %v", got)
	}
}

func TestOptedInBuildReplacers_optedInWorktree(t *testing.T) {
	dir := setupOptedInProject(t, []string{"vite"})
	site := &config.Site{Name: "site", Path: dir, Framework: "laravel"}
	wt := filepath.Join(dir, "wt-main")
	if err := os.MkdirAll(wt, 0755); err != nil {
		t.Fatal(err)
	}
	got := OptedInBuildReplacers(site, wt)
	if len(got) != 1 || got[0] != "vite" {
		t.Errorf("want [vite] for opted-in vite worktree, got %v", got)
	}
}

func TestOptedInBuildReplacers_optedInParent(t *testing.T) {
	dir := setupOptedInProject(t, []string{"vite"})
	site := &config.Site{Name: "site", Path: dir, Framework: "laravel"}
	got := OptedInBuildReplacers(site, dir) // parent path == site.Path
	if len(got) != 1 || got[0] != "vite" {
		t.Errorf("want [vite] for opted-in vite parent, got %v", got)
	}
}

func TestOptedInBuildReplacers_notOptedIn(t *testing.T) {
	dir := setupOptedInProject(t, []string{"queue"})
	site := &config.Site{Name: "site", Path: dir, Framework: "laravel"}
	if got := OptedInBuildReplacers(site, dir); len(got) != 0 {
		t.Errorf("vite not opted in; want [], got %v", got)
	}
}

func TestOptedInBuildReplacers_skipsWhenNotPerWorktree(t *testing.T) {
	// A replaces_build worker that lacks per_worktree:true must NOT suppress
	// the build prompt for a worktree (it wouldn't run there). It should
	// still suppress for the parent.
	dir := setupOptedInProject(t, []string{"docs"})
	proj, _ := config.LoadProjectConfig(dir)
	proj.CustomWorkers["docs"] = config.FrameworkWorker{
		Command:       "npm run docs",
		Host:          true,
		ReplacesBuild: true,
		// PerWorktree intentionally unset (defaults to false)
	}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}
	site := &config.Site{Name: "site", Path: dir, Framework: "laravel"}

	wt := filepath.Join(dir, "wt-main")
	_ = os.MkdirAll(wt, 0755)
	if got := OptedInBuildReplacers(site, wt); len(got) != 0 {
		t.Errorf("docs is parent-only; worktree should not see it as a replacer, got %v", got)
	}
	if got := OptedInBuildReplacers(site, dir); len(got) != 1 || got[0] != "docs" {
		t.Errorf("docs should replace build at the parent, got %v", got)
	}
}

func TestOptedInBuildReplacers_workerMissingFlag(t *testing.T) {
	// Worker is opted in and per_worktree but doesn't declare replaces_build.
	// It should NOT suppress the build prompt.
	dir := setupOptedInProject(t, []string{"watcher"})
	proj, _ := config.LoadProjectConfig(dir)
	tr := true
	proj.CustomWorkers["watcher"] = config.FrameworkWorker{
		Command:     "npm run watch",
		Host:        true,
		PerWorktree: &tr,
	}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}
	site := &config.Site{Name: "site", Path: dir, Framework: "laravel"}
	wt := filepath.Join(dir, "wt-main")
	_ = os.MkdirAll(wt, 0755)
	if got := OptedInBuildReplacers(site, wt); len(got) != 0 {
		t.Errorf("watcher lacks replaces_build; build must still run, got %v", got)
	}
}
