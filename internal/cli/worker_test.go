package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// resolveSiteAndFramework must fall back to the parent site when cwd is a
// git worktree checkout under a registered repo, so `lerd worker start`
// commands invoked from inside a worktree don't error with "not a registered
// site". This pins the new ParentSiteForWorktreeDir branch.
func TestResolveSiteAndFramework_worktreeFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	sitePath := filepath.Join(tmp, "acme")
	if err := os.MkdirAll(filepath.Join(sitePath, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(t.TempDir(), "feat-x-checkout")
	if err := os.Mkdir(checkout, 0755); err != nil {
		t.Fatal(err)
	}
	wtMeta := filepath.Join(sitePath, ".git", "worktrees", "feat-x")
	os.MkdirAll(wtMeta, 0755)
	os.WriteFile(filepath.Join(wtMeta, "HEAD"), []byte("ref: refs/heads/feat-x\n"), 0644)
	os.WriteFile(filepath.Join(wtMeta, "gitdir"), []byte(filepath.Join(checkout, ".git")+"\n"), 0644)

	if err := config.AddSite(config.Site{
		Name: "acme", Path: sitePath, Domains: []string{"acme.test"},
		PHPVersion: "8.4", Framework: "no-such-framework",
	}); err != nil {
		t.Fatal(err)
	}

	site, _, _, err := resolveSiteAndFramework(checkout)
	// We expect the framework lookup to fail (no such framework registered),
	// not the "not a registered site" branch. A non-nil site on the err side
	// would also be acceptable, but the key signal is that we got past the
	// FindSiteByPath miss via the worktree fallback.
	if err == nil {
		// Framework happened to resolve; the fallback worked and the rest succeeded.
		if site == nil || site.Name != "acme" {
			t.Errorf("expected fallback to return parent site acme, got %+v", site)
		}
		return
	}
	if strings.Contains(err.Error(), "not a registered site") {
		t.Errorf("worktree fallback did not fire; got registered-site error: %v", err)
	}
}

func TestWorkerAdd_Project(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644)

	proj, _ := config.LoadProjectConfig(dir)
	if proj.CustomWorkers == nil {
		proj.CustomWorkers = make(map[string]config.FrameworkWorker)
	}
	proj.CustomWorkers["pulse"] = config.FrameworkWorker{
		Label:   "Pulse",
		Command: "php artisan pulse:work",
	}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}

	got, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	w, ok := got.CustomWorkers["pulse"]
	if !ok {
		t.Fatal("expected pulse in custom_workers")
	}
	if w.Command != "php artisan pulse:work" {
		t.Errorf("command = %q, want %q", w.Command, "php artisan pulse:work")
	}
	if w.Label != "Pulse" {
		t.Errorf("label = %q, want %q", w.Label, "Pulse")
	}
}

func TestWorkerAdd_UpdateExisting(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644)

	proj, _ := config.LoadProjectConfig(dir)
	proj.CustomWorkers = map[string]config.FrameworkWorker{
		"pulse": {Command: "php artisan pulse:work"},
	}
	config.SaveProjectConfig(dir, proj)

	// Update the command.
	proj, _ = config.LoadProjectConfig(dir)
	proj.CustomWorkers["pulse"] = config.FrameworkWorker{
		Command: "php artisan pulse:work --rest=1",
		Label:   "Pulse Updated",
	}
	config.SaveProjectConfig(dir, proj)

	got, _ := config.LoadProjectConfig(dir)
	w := got.CustomWorkers["pulse"]
	if w.Command != "php artisan pulse:work --rest=1" {
		t.Errorf("command not updated: %q", w.Command)
	}
	if w.Label != "Pulse Updated" {
		t.Errorf("label not updated: %q", w.Label)
	}
}

func TestWorkerAdd_WithCheck(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644)

	proj, _ := config.LoadProjectConfig(dir)
	proj.CustomWorkers = map[string]config.FrameworkWorker{
		"pulse": {
			Command: "php artisan pulse:work",
			Check:   &config.FrameworkRule{Composer: "laravel/pulse"},
		},
	}
	config.SaveProjectConfig(dir, proj)

	got, _ := config.LoadProjectConfig(dir)
	w := got.CustomWorkers["pulse"]
	if w.Check == nil {
		t.Fatal("expected check to be set")
	}
	if w.Check.Composer != "laravel/pulse" {
		t.Errorf("check.composer = %q, want %q", w.Check.Composer, "laravel/pulse")
	}
}

func TestWorkerAdd_WithProxy(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644)

	proj, _ := config.LoadProjectConfig(dir)
	proj.CustomWorkers = map[string]config.FrameworkWorker{
		"ws": {
			Command: "php artisan ws:serve",
			Proxy: &config.WorkerProxy{
				Path:        "/ws",
				PortEnvKey:  "WS_PORT",
				DefaultPort: 6001,
			},
		},
	}
	config.SaveProjectConfig(dir, proj)

	got, _ := config.LoadProjectConfig(dir)
	w := got.CustomWorkers["ws"]
	if w.Proxy == nil {
		t.Fatal("expected proxy to be set")
	}
	if w.Proxy.Path != "/ws" {
		t.Errorf("proxy.path = %q, want %q", w.Proxy.Path, "/ws")
	}
	if w.Proxy.PortEnvKey != "WS_PORT" {
		t.Errorf("proxy.port_env_key = %q", w.Proxy.PortEnvKey)
	}
	if w.Proxy.DefaultPort != 6001 {
		t.Errorf("proxy.default_port = %d, want 6001", w.Proxy.DefaultPort)
	}
}

func TestWorkerRemove_Project(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644)

	proj, _ := config.LoadProjectConfig(dir)
	proj.CustomWorkers = map[string]config.FrameworkWorker{
		"pulse": {Command: "php artisan pulse:work"},
		"other": {Command: "php artisan other:work"},
	}
	config.SaveProjectConfig(dir, proj)

	// Remove pulse.
	proj, _ = config.LoadProjectConfig(dir)
	delete(proj.CustomWorkers, "pulse")
	config.SaveProjectConfig(dir, proj)

	got, _ := config.LoadProjectConfig(dir)
	if _, ok := got.CustomWorkers["pulse"]; ok {
		t.Error("pulse should have been removed")
	}
	if _, ok := got.CustomWorkers["other"]; !ok {
		t.Error("other should still be present")
	}
}

func TestWorkerRemove_NilsEmptyMap(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644)

	proj, _ := config.LoadProjectConfig(dir)
	proj.CustomWorkers = map[string]config.FrameworkWorker{
		"pulse": {Command: "php artisan pulse:work"},
	}
	config.SaveProjectConfig(dir, proj)

	// Remove the last worker and nil out.
	proj, _ = config.LoadProjectConfig(dir)
	delete(proj.CustomWorkers, "pulse")
	proj.CustomWorkers = nil
	config.SaveProjectConfig(dir, proj)

	// Verify custom_workers is absent from YAML.
	data, _ := os.ReadFile(filepath.Join(dir, ".lerd.yaml"))
	if strings.Contains(string(data), "custom_workers") {
		t.Error("custom_workers should be omitted from YAML when nil")
	}
}

func TestWorkerRemove_NotFound(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644)

	proj, _ := config.LoadProjectConfig(dir)
	if _, exists := proj.CustomWorkers["nonexistent"]; exists {
		t.Error("nonexistent worker should not be found")
	}
}

func TestWorkerAdd_Global(t *testing.T) {
	// Use a temp dir as the frameworks dir.
	dir := t.TempDir()
	origDir := config.FrameworksDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	fwDir := filepath.Join(dir, "lerd", "frameworks")
	os.MkdirAll(fwDir, 0755)

	fw := &config.Framework{
		Name: "testfw",
		Workers: map[string]config.FrameworkWorker{
			"pulse": {Command: "php artisan pulse:work", Label: "Pulse"},
		},
	}
	if err := config.SaveFramework(fw); err != nil {
		t.Fatal(err)
	}

	loaded := config.LoadUserFramework("testfw")
	if loaded == nil {
		t.Fatal("expected to load saved framework")
	}
	w, ok := loaded.Workers["pulse"]
	if !ok {
		t.Fatal("expected pulse worker")
	}
	if w.Command != "php artisan pulse:work" {
		t.Errorf("command = %q", w.Command)
	}

	// Verify frameworks dir changed.
	_ = origDir
}

func TestWorkerRemove_Global(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	fwDir := filepath.Join(dir, "lerd", "frameworks")
	os.MkdirAll(fwDir, 0755)

	fw := &config.Framework{
		Name: "testfw",
		Workers: map[string]config.FrameworkWorker{
			"pulse": {Command: "php artisan pulse:work"},
			"other": {Command: "php artisan other:work"},
		},
	}
	config.SaveFramework(fw)

	// Remove pulse.
	loaded := config.LoadUserFramework("testfw")
	delete(loaded.Workers, "pulse")
	if len(loaded.Workers) == 0 {
		loaded.Workers = nil
	}
	config.SaveFramework(loaded)

	reloaded := config.LoadUserFramework("testfw")
	if _, ok := reloaded.Workers["pulse"]; ok {
		t.Error("pulse should have been removed")
	}
	if _, ok := reloaded.Workers["other"]; !ok {
		t.Error("other should still be present")
	}
}

// hostWorkerNotReadyMsg returns a runtime-agnostic JS-deps message only for a
// host worker on a node project, and "" otherwise so callers fall back to their
// own generic dependency message.
func TestHostWorkerNotReadyMsg(t *testing.T) {
	nodeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodeDir, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	plainDir := t.TempDir()

	hostWorker := config.FrameworkWorker{Host: true, Check: &config.FrameworkRule{File: "node_modules/vite"}}

	msg := hostWorkerNotReadyMsg("vite", nodeDir, hostWorker)
	for _, want := range []string{"JS dependencies are not installed", "lerd setup", "lerd worker start vite"} {
		if !strings.Contains(msg, want) {
			t.Errorf("host node-worker message missing %q: %s", want, msg)
		}
	}
	// The remedy must stay runtime-agnostic so it doesn't misdirect bun/yarn/pnpm projects.
	if strings.Contains(msg, "npm ci") {
		t.Errorf("message should not hardcode npm ci: %s", msg)
	}

	// Not a node project: no JS-deps claim, caller keeps its generic message.
	if got := hostWorkerNotReadyMsg("vite", plainDir, hostWorker); got != "" {
		t.Errorf("non-node project should yield no host message, got: %s", got)
	}
	// Non-host worker: never a JS-deps message even on a node project.
	containerWorker := config.FrameworkWorker{Check: &config.FrameworkRule{Composer: "vendor/pkg"}}
	if got := hostWorkerNotReadyMsg("queue", nodeDir, containerWorker); got != "" {
		t.Errorf("non-host worker should yield no host message, got: %s", got)
	}
}

// End-to-end gate: WorkerStartForSite runs workerStartPreflight first and returns
// its error before touching systemd/podman, so a node project (package.json, no
// node_modules) with a vite host worker must be refused with the actionable
// JS-deps message and cause no side effects. This mirrors what a real
// `lerd worker start vite` surfaces on a freshly linked project.
func TestWorkerStartForSiteRefusesViteWithoutDeps(t *testing.T) {
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(siteDir, "package.json"), []byte(`{"scripts":{"dev":"vite"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	vite := config.FrameworkWorker{
		Host:    true,
		Command: "npm run dev",
		Restart: "on-failure",
		Check:   &config.FrameworkRule{File: "node_modules/vite"},
	}

	err := WorkerStartForSite("acme", siteDir, "8.5", "vite", vite, false)
	if err == nil {
		t.Fatal("expected WorkerStartForSite to refuse vite without node_modules, got nil")
	}
	t.Logf("surfaced message: %s", err)
	for _, want := range []string{"JS dependencies are not installed", "lerd setup", "lerd worker start vite"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("preflight error missing %q: %s", want, err)
		}
	}

	// No unit file should have been written for the refused worker.
	unit, _ := workerNames("acme", siteDir, "vite")
	if _, statErr := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "systemd", "user", unit+".service")); statErr == nil {
		t.Errorf("a unit file was written for a refused worker: %s", unit)
	}
}
