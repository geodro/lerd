package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/siteops"
)

// TestStopSiteWorkersHookRegistered guards the wiring that makes every unlink
// path (CLI, MCP, parked-watcher) stop a site's workers. Without it the
// host-proxy dev server leaks on the MCP and watcher paths.
func TestStopSiteWorkersHookRegistered(t *testing.T) {
	if siteops.StopSiteWorkers == nil {
		t.Fatal("cli init must register siteops.StopSiteWorkers")
	}
}

func TestHostProxyWorkerUnit_sharedWithConfig(t *testing.T) {
	if hostProxyWorkerUnit("myapp") != config.HostProxyWorkerUnit("myapp") {
		t.Error("cli and config must agree on the host-proxy worker unit name")
	}
}

func writePkgJSON(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAvailableDevScripts_priorityOrder(t *testing.T) {
	dir := t.TempDir()
	writePkgJSON(t, dir, `{"scripts":{"start":"node x","dev":"vite","start:dev":"nest start --watch","build":"vite build"}}`)
	got := AvailableDevScripts(dir)
	want := []string{"npm run start:dev", "npm run dev", "npm run start"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestAvailableDevScripts_noPackageJSON(t *testing.T) {
	if got := AvailableDevScripts(t.TempDir()); got != nil {
		t.Errorf("expected nil for missing package.json, got %v", got)
	}
}

func TestRunsVite(t *testing.T) {
	m := &packageManifest{Scripts: map[string]string{"dev": "vite", "start:dev": "nest start --watch"}}
	cases := []struct {
		manifest *packageManifest
		command  string
		want     bool
	}{
		{m, "npm run dev", true},        // resolves script to vite
		{m, "npm run start:dev", false}, // nest, not vite
		{m, "vite --port 5173", true},   // custom command names vite
		{m, "npm run build", false},     // script not present
		{nil, "npm run dev", false},     // no manifest, not a vite command
		{nil, "vite", true},             // no manifest but command is vite
		{m, "", false},                  // proxy-only mode
	}
	for _, c := range cases {
		if got := c.manifest.runsVite(c.command); got != c.want {
			t.Errorf("runsVite(%q) = %v, want %v", c.command, got, c.want)
		}
	}
}

func TestHostProxyGate(t *testing.T) {
	cases := []struct {
		name                                 string
		command                              string
		disabled, skipConfirm, approved, tty bool
		wantProceed, wantPrompt              bool
	}{
		{"disabled blocks even when approved", "npm run dev", true, false, true, true, false, false},
		{"proxy-only empty command proceeds", "", false, false, false, false, true, false},
		{"disabled still allows proxy-only", "", true, false, false, false, true, false},
		{"already approved proceeds", "npm run dev", false, false, true, false, true, false},
		{"skip_confirmation proceeds", "npm run dev", false, true, false, false, true, false},
		{"interactive unapproved needs prompt", "npm run dev", false, false, false, true, false, true},
		{"non-interactive unapproved is refused", "npm run dev", false, false, false, false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			proceed, prompt, reason := hostProxyGate(c.command, c.disabled, c.skipConfirm, c.approved, c.tty)
			if proceed != c.wantProceed || prompt != c.wantPrompt {
				t.Errorf("hostProxyGate = (proceed %v, prompt %v), want (%v, %v)", proceed, prompt, c.wantProceed, c.wantPrompt)
			}
			if !proceed && !prompt && reason == "" {
				t.Errorf("a refusal must carry a reason")
			}
		})
	}
}

func TestHostProxyGate_PreApprovedOverridesPrompt(t *testing.T) {
	hostProxyPreApproved = true
	defer func() { hostProxyPreApproved = false }()
	if proceed, prompt, _ := hostProxyGate("npm run dev", false, false, false, true); !proceed || prompt {
		t.Errorf("wizard pre-approval should proceed without prompting; got proceed=%v prompt=%v", proceed, prompt)
	}
}

func TestPortFromCommand(t *testing.T) {
	cases := map[string]int{
		"vite --port 4000":       4000,
		"ng serve --port=4300":   4300,
		"PORT=8080 node main.js": 8080,
		"npm run dev":            0,
	}
	for cmd, want := range cases {
		if got := portFromCommand(cmd); got != want {
			t.Errorf("portFromCommand(%q) = %d, want %d", cmd, got, want)
		}
	}
}

func TestBuildProjectServices_builtins(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got := buildProjectServices([]string{"redis", "mysql"}, &config.ProjectConfig{})
	if len(got) != 2 {
		t.Fatalf("expected 2 services, got %d: %+v", len(got), got)
	}
	if got[0].Name != "redis" || got[0].Preset != "" || got[0].Custom != nil {
		t.Errorf("redis built-in mapped wrong: %+v", got[0])
	}
	if got[1].Name != "mysql" {
		t.Errorf("mysql mapped wrong: %+v", got[1])
	}
}

func TestBuildHostProxyCommand_injectsPortAndHost(t *testing.T) {
	got := buildHostProxyCommand(&config.ProxyConfig{Command: "npm run start:dev", Port: 3000})
	want := "env PORT=3000 HOST=0.0.0.0 npm run start:dev"
	if got != want {
		t.Errorf("buildHostProxyCommand = %q, want %q", got, want)
	}
}

func TestBuildHostProxyCommand_customEnvKeys(t *testing.T) {
	got := buildHostProxyCommand(&config.ProxyConfig{Command: "node server.js", Port: 4000, PortEnvKey: "APP_PORT", HostEnvKey: "HOSTNAME"})
	want := "env APP_PORT=4000 HOSTNAME=0.0.0.0 node server.js"
	if got != want {
		t.Errorf("buildHostProxyCommand = %q, want %q", got, want)
	}
}

func TestBuildHostProxyCommand_injectHostFalseOptsOut(t *testing.T) {
	// inject_host: false suppresses the HOST injection but keeps the port.
	injectHost := false
	got := buildHostProxyCommand(&config.ProxyConfig{Command: "npm run dev", Port: 3000, InjectHost: &injectHost})
	want := "env PORT=3000 npm run dev"
	if got != want {
		t.Errorf("buildHostProxyCommand = %q, want %q", got, want)
	}
}

func TestBuildHostProxyCommand_injectHostTrueExplicitInjects(t *testing.T) {
	// inject_host: true is the same as the default: HOST is injected.
	injectHost := true
	got := buildHostProxyCommand(&config.ProxyConfig{Command: "npm run dev", Port: 3000, InjectHost: &injectHost})
	want := "env PORT=3000 HOST=0.0.0.0 npm run dev"
	if got != want {
		t.Errorf("buildHostProxyCommand = %q, want %q", got, want)
	}
}

func TestBuildHostProxyCommand_proxyOnlyMode(t *testing.T) {
	// No command means proxy-only: lerd supervises nothing.
	if got := buildHostProxyCommand(&config.ProxyConfig{Port: 3000}); got != "" {
		t.Errorf("buildHostProxyCommand with no command = %q, want empty", got)
	}
}

func TestFirstFreePort(t *testing.T) {
	// 3000 and 3001 taken → first free is 3002.
	taken := map[int]bool{3000: true, 3001: true}
	if got := firstFreePort(3000, func(p int) bool { return taken[p] }); got != 3002 {
		t.Errorf("firstFreePort = %d, want 3002", got)
	}
	// Nothing taken → start is returned unchanged.
	if got := firstFreePort(5173, func(int) bool { return false }); got != 5173 {
		t.Errorf("firstFreePort = %d, want 5173 (start, none taken)", got)
	}
	// Below-range start is clamped to 1.
	if got := firstFreePort(0, func(int) bool { return false }); got != 1 {
		t.Errorf("firstFreePort(0) = %d, want 1", got)
	}
}

func TestHostProxyWorkerForPort_usesGivenPort(t *testing.T) {
	proxy := &config.ProxyConfig{Command: "npm run start:dev", Port: 3000}
	w, ok := hostProxyWorkerForPort(proxy, 3101)
	if !ok {
		t.Fatal("expected a worker for a non-empty command")
	}
	if w.Command != "env PORT=3101 HOST=0.0.0.0 npm run start:dev" {
		t.Errorf("worker command = %q, want the worktree port 3101 injected", w.Command)
	}
	if !w.Host || w.Restart != "always" {
		t.Errorf("expected host + always-restart worker, got Host=%v Restart=%q", w.Host, w.Restart)
	}
	// Proxy-only mode (no command) yields no worker.
	if _, ok := hostProxyWorkerForPort(&config.ProxyConfig{Port: 3000}, 3101); ok {
		t.Error("expected no worker in proxy-only mode")
	}
}

func TestWorktreeHostPort_readsPersistedEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("PORT=4321\nFOO=bar\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := WorktreeHostPort(3000, dir, "PORT"); got != 4321 {
		t.Errorf("WorktreeHostPort = %d, want 4321 (persisted value reused)", got)
	}
}

func TestWorktreeHostPort_allocatesAndPersists(t *testing.T) {
	dir := t.TempDir()
	// .env exists but has no PORT yet → a fresh port is allocated and written.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("FOO=bar\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got := WorktreeHostPort(3000, dir, "PORT")
	if got <= 3000 {
		t.Errorf("WorktreeHostPort = %d, want a port above the parent's 3000", got)
	}
	// Second call must reuse the now-persisted value.
	if again := WorktreeHostPort(3000, dir, "PORT"); again != got {
		t.Errorf("WorktreeHostPort second call = %d, want stable %d", again, got)
	}
}
