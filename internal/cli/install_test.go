package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

func TestIsShell_fish(t *testing.T) {
	if !isShell("/usr/bin/fish", "fish") {
		t.Error("expected /usr/bin/fish to match fish")
	}
}

func TestIsShell_zsh(t *testing.T) {
	if !isShell("/bin/zsh", "zsh") {
		t.Error("expected /bin/zsh to match zsh")
	}
}

func TestIsShell_mismatch(t *testing.T) {
	if isShell("/bin/bash", "zsh") {
		t.Error("expected /bin/bash not to match zsh")
	}
}

func TestIsShell_empty(t *testing.T) {
	if isShell("", "bash") {
		t.Error("expected empty shell not to match")
	}
}

func TestPortPreflightConflicts(t *testing.T) {
	// Isolate config so nginx ports fall back to the 80/443 defaults and the
	// DNS check is the fixed 5300, making CollectPortChecks deterministic.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	notRunning := func(string) bool { return false }
	running := func(string) bool { return true }
	dnsDown := func() bool { return false }
	dnsUp := func() bool { return true }

	hasPort := func(cs []PortCheck, port string) bool {
		for _, c := range cs {
			if c.Port == port {
				return true
			}
		}
		return false
	}

	t.Run("foreign listener on 80 is a conflict", func(t *testing.T) {
		list := "herd-nginx 1234 herd 6u IPv4 TCP 0.0.0.0:80 (LISTEN)"
		got := portPreflightConflicts(list, notRunning, dnsDown)
		if !hasPort(got, "80") {
			t.Fatalf("expected port 80 conflict, got %+v", got)
		}
	})

	t.Run("lerd-nginx already running is not a conflict", func(t *testing.T) {
		list := "conmon 1234 sdp 6u IPv4 TCP 0.0.0.0:80 (LISTEN)"
		got := portPreflightConflicts(list, running, dnsUp)
		if len(got) != 0 {
			t.Fatalf("expected no conflicts when lerd owns the ports, got %+v", got)
		}
	})

	t.Run("answering dnsmasq holding 5300 is not a conflict", func(t *testing.T) {
		list := "dnsmasq 60498 sdp 5u IPv4 TCP 127.0.0.1:5300 (LISTEN)"
		got := portPreflightConflicts(list, notRunning, dnsUp)
		if hasPort(got, "5300") {
			t.Fatalf("expected 5300 suppressed when dns is answering, got %+v", got)
		}
	})

	t.Run("all ports free yields no conflicts", func(t *testing.T) {
		got := portPreflightConflicts("", notRunning, dnsDown)
		if len(got) != 0 {
			t.Fatalf("expected no conflicts on an empty listener list, got %+v", got)
		}
	})
}

func TestEnsurePortForwarding(t *testing.T) {
	// Should not error on any platform
	if err := ensurePortForwarding(); err != nil {
		t.Errorf("ensurePortForwarding error: %v", err)
	}
}

func TestNeedsDNSServiceInstall(t *testing.T) {
	if runtime.GOOS == "linux" {
		if needsDNSServiceInstall() {
			t.Error("needsDNSServiceInstall should return false on linux")
		}
	}
	// On macOS the result depends on whether plists exist — skip assertion
}

func TestIsDNSContainerUnit(t *testing.T) {
	if runtime.GOOS == "linux" {
		if !isDNSContainerUnit() {
			t.Error("isDNSContainerUnit should return true on linux")
		}
	} else {
		if isDNSContainerUnit() {
			t.Error("isDNSContainerUnit should return false on macOS")
		}
	}
}

func TestPullDNSImages(t *testing.T) {
	jobs := pullDNSImages()
	if runtime.GOOS == "linux" {
		if len(jobs) == 0 {
			t.Error("pullDNSImages should return build jobs on linux")
		}
	} else {
		if len(jobs) != 0 {
			t.Error("pullDNSImages should return nil on macOS")
		}
	}
}

func TestFileChangedBy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")

	// Missing before, written after -> changed.
	changed, err := fileChangedBy(path, func() error { return os.WriteFile(path, []byte("a"), 0644) })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !changed {
		t.Error("creating the file should count as a change")
	}

	// Same content rewritten -> not changed.
	changed, err = fileChangedBy(path, func() error { return os.WriteFile(path, []byte("a"), 0644) })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if changed {
		t.Error("rewriting identical content should not count as a change")
	}

	// Different content -> changed.
	changed, err = fileChangedBy(path, func() error { return os.WriteFile(path, []byte("b"), 0644) })
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !changed {
		t.Error("new content should count as a change")
	}

	// mutate error is propagated and reported as no change.
	wantErr := errors.New("boom")
	changed, err = fileChangedBy(path, func() error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Errorf("expected mutate error to propagate, got %v", err)
	}
	if changed {
		t.Error("a failed mutate should report no change")
	}
}

func TestInstallAutostart(t *testing.T) {
	installAutostart()
}

func TestInstallCleanupScript(t *testing.T) {
	installCleanupScript()
}

func TestAddShellShims_LaravelShim(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)
	// Clear COMPOSER_HOME so the default path is used.
	t.Setenv("COMPOSER_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	// addShellShims expects a shell env var for the PATH block.
	t.Setenv("SHELL", "/bin/sh")

	if err := addShellShims(false); err != nil {
		t.Fatalf("addShellShims: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(binDir, "laravel"))
	if err != nil {
		t.Fatalf("laravel shim not created: %v", err)
	}

	shim := string(data)
	if !strings.HasPrefix(shim, "#!/bin/sh\n") {
		t.Errorf("laravel shim missing shebang, got: %q", shim)
	}
	expectedComposerHome := filepath.Join(tmp, ".config", "composer")
	expectedPath := expectedComposerHome + "/vendor/bin/laravel"
	if !strings.Contains(shim, expectedPath) {
		t.Errorf("laravel shim does not reference %q, got:\n%s", expectedPath, shim)
	}
}

func TestRefreshUnreferencedCustomQuadlets_globalCustomServiceGetsV6Pair(t *testing.T) {
	// Simulates a preset like mongo-express installed globally via
	// `lerd service preset install`: a yaml in CustomServicesDir() with
	// loopback publish ports, but no site .lerd.yaml references it.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	origReload := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = origReload })
	podman.DaemonReloadFn = func() error { return nil }

	svc := &config.CustomService{
		Name:  "mongo-express",
		Image: "docker.io/library/mongo-express:latest",
		Ports: []string{"127.0.0.1:8082:8081"},
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	refreshUnreferencedCustomQuadlets(map[string]bool{}, nil)

	path := filepath.Join(config.QuadletDir(), "lerd-mongo-express.container")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("quadlet not written at %s: %v", path, err)
	}
	got := string(data)
	if !strings.Contains(got, "PublishPort=127.0.0.1:8082:8081") {
		t.Errorf("v4 bind missing from rewritten quadlet:\n%s", got)
	}
	if !strings.Contains(got, "PublishPort=[::1]:8082:8081") {
		t.Errorf("v6 pair missing — PairIPv6Binds did not apply during refresh:\n%s", got)
	}
}

func TestRefreshUnreferencedCustomQuadlets_skipsSeenServices(t *testing.T) {
	// If the per-site loop already handled a service, the second pass must
	// not overwrite its quadlet, so an overridden image or extra port is
	// preserved.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	origReload := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = origReload })
	podman.DaemonReloadFn = func() error { return nil }

	svc := &config.CustomService{
		Name:  "mongo-express",
		Image: "docker.io/library/mongo-express:latest",
		Ports: []string{"127.0.0.1:8082:8081"},
	}
	if err := config.SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}

	seen := map[string]bool{"mongo-express": true}
	refreshUnreferencedCustomQuadlets(seen, nil)

	path := filepath.Join(config.QuadletDir(), "lerd-mongo-express.container")
	if _, err := os.Stat(path); err == nil {
		t.Errorf("quadlet at %s should not be written when service is already in seenSvc", path)
	}
}

func TestRefreshUnreferencedCustomQuadlets_rewritesCustomContainerSite(t *testing.T) {
	// Custom-container sites (ContainerPort > 0) don't publish ports, so
	// PairIPv6Binds is a no-op. The refresh must still write a fresh
	// quadlet on disk so a migrated container restarts from current state.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	projectDir := filepath.Join(tmp, "my-app")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	reg := &config.SiteRegistry{
		Sites: []config.Site{
			{
				Name:          "my-app",
				Domains:       []string{"my-app.test"},
				Path:          projectDir,
				ContainerPort: 3000,
			},
		},
	}

	refreshUnreferencedCustomQuadlets(map[string]bool{}, reg)

	path := filepath.Join(config.QuadletDir(), "lerd-custom-my-app.container")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("custom-container quadlet not written at %s: %v", path, err)
	}
	got := string(data)
	if !strings.Contains(got, "Network=lerd") {
		t.Errorf("expected Network=lerd in custom-container quadlet:\n%s", got)
	}
	if !strings.Contains(got, "ContainerName=lerd-custom-my-app") {
		t.Errorf("expected ContainerName=lerd-custom-my-app:\n%s", got)
	}
}

func TestRefreshUnreferencedCustomQuadlets_rewritesFrankenPHPSite(t *testing.T) {
	// FrankenPHP sites (Runtime=="frankenphp") don't publish ports either, but
	// the per-site loop that generates vhosts does not rewrite their quadlets.
	// The refresh pass must emit a fresh lerd-fp-<name>.container on disk so
	// the v4→v6 network migration (and any other quadlet-schema change) lands
	// on the FrankenPHP container when install restarts it.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	projectDir := filepath.Join(tmp, "my-app")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	reg := &config.SiteRegistry{
		Sites: []config.Site{
			{
				Name:       "my-app",
				Domains:    []string{"my-app.test"},
				Path:       projectDir,
				PHPVersion: "8.4",
				Runtime:    "frankenphp",
			},
		},
	}

	refreshUnreferencedCustomQuadlets(map[string]bool{}, reg)

	path := filepath.Join(config.QuadletDir(), "lerd-fp-my-app.container")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("frankenphp quadlet not written at %s: %v", path, err)
	}
	got := string(data)
	wantFragments := []string{
		"ContainerName=lerd-fp-my-app",
		"Image=localhost/lerd-frankenphp84:local",
		"Network=lerd",
		"Volume=" + projectDir + ":" + projectDir + ":rw",
	}
	for _, s := range wantFragments {
		if !strings.Contains(got, s) {
			t.Errorf("quadlet missing %q:\n%s", s, got)
		}
	}
}

func TestRefreshUnreferencedCustomQuadlets_skipsPausedAndIgnoredSites(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	reg := &config.SiteRegistry{
		Sites: []config.Site{
			{Name: "paused-app", Path: tmp, ContainerPort: 3001, Paused: true},
			{Name: "ignored-app", Path: tmp, ContainerPort: 3002, Ignored: true},
		},
	}
	refreshUnreferencedCustomQuadlets(map[string]bool{}, reg)

	for _, name := range []string{"lerd-custom-paused-app", "lerd-custom-ignored-app"} {
		path := filepath.Join(config.QuadletDir(), name+".container")
		if _, err := os.Stat(path); err == nil {
			t.Errorf("quadlet %s should not be written for paused/ignored site", path)
		}
	}
}

func TestAddShellShims_LaravelShimRespectsComposerHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)

	customHome := filepath.Join(tmp, "custom-composer")
	t.Setenv("COMPOSER_HOME", customHome)

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SHELL", "/bin/sh")

	if err := addShellShims(false); err != nil {
		t.Fatalf("addShellShims: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(binDir, "laravel"))
	if err != nil {
		t.Fatalf("laravel shim not created: %v", err)
	}

	shim := string(data)
	expectedPath := customHome + "/vendor/bin/laravel"
	if !strings.Contains(shim, expectedPath) {
		t.Errorf("laravel shim should use COMPOSER_HOME=%q, got:\n%s", customHome, shim)
	}
}

func TestAddShellShims_NodeShimChecksDefaultAlias(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/sh")

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := addShellShims(true); err != nil {
		t.Fatalf("addShellShims: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(binDir, "npm"))
	if err != nil {
		t.Fatalf("npm shim not created: %v", err)
	}
	shim := string(data)
	if !strings.Contains(shim, `exec "$LERD" npm "$@"`) {
		t.Errorf("npm shim should delegate to lerd binary first, got:\n%s", shim)
	}
	if !strings.Contains(shim, `"$FNM" exec --using=default -- true`) {
		t.Errorf("npm shim should probe the default alias in the fallback path, got:\n%s", shim)
	}
	if !strings.Contains(shim, "No Node.js version available via lerd") {
		t.Errorf("npm shim should print friendly fallback hint, got:\n%s", shim)
	}
}

func TestAddShellShims_ComposerShimDelegatesToLerd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/sh")

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := addShellShims(false); err != nil {
		t.Fatalf("addShellShims: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(binDir, "composer"))
	if err != nil {
		t.Fatalf("composer shim not created: %v", err)
	}
	shim := string(data)
	if !strings.Contains(shim, `exec "$LERD" composer "$@"`) {
		t.Errorf("composer shim should delegate to lerd composer first, got:\n%s", shim)
	}
	if !strings.Contains(shim, "composer.phar") {
		t.Errorf("composer shim should keep a composer.phar fallback path, got:\n%s", shim)
	}
}

func TestDetectSystemNode_findsNvmDirEvenWhenPathIsEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("PATH", filepath.Join(tmp, "lerd", "bin"))

	nvm := filepath.Join(tmp, ".nvm", "versions", "node", "v22.0.0")
	if err := os.MkdirAll(nvm, 0755); err != nil {
		t.Fatal(err)
	}
	got := detectSystemNode()
	if got == "" {
		t.Fatal("detectSystemNode returned empty even though ~/.nvm contains a version")
	}
	if !strings.Contains(got, ".nvm") {
		t.Errorf("expected detectSystemNode to point at the nvm dir, got %q", got)
	}
}

func TestDetectSystemNode_findsNpmInPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	binDir := filepath.Join(tmp, "shim-bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "npm"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	got := detectSystemNode()
	if got == "" {
		t.Fatal("detectSystemNode returned empty even though npm is on PATH")
	}
	if !strings.HasSuffix(got, "/npm") {
		t.Errorf("expected detectSystemNode to find npm, got %q", got)
	}
}

// TestAddShellShims_OptOutRemovesNodeShims covers re-running `lerd install`
// after a previous managed-node install: answering "no" to the prompt must
// actually unblock system node, which means deleting the fnm-backed shims
// that would otherwise keep masking it in PATH.
func TestAddShellShims_OptOutRemovesNodeShims(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/sh")

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := addShellShims(true); err != nil {
		t.Fatalf("addShellShims(true): %v", err)
	}
	for _, bin := range []string{"node", "npm", "npx"} {
		if _, err := os.Stat(filepath.Join(binDir, bin)); err != nil {
			t.Fatalf("%s shim missing after addShellShims(true): %v", bin, err)
		}
	}

	if err := addShellShims(false); err != nil {
		t.Fatalf("addShellShims(false): %v", err)
	}
	for _, bin := range []string{"node", "npm", "npx"} {
		if _, err := os.Stat(filepath.Join(binDir, bin)); err == nil {
			t.Errorf("%s shim should be removed when manageNode=false", bin)
		} else if !os.IsNotExist(err) {
			t.Errorf("%s shim stat: %v", bin, err)
		}
	}

	// Non-node shims must be preserved across the opt-out.
	for _, bin := range []string{"php", "composer", "laravel"} {
		if _, err := os.Stat(filepath.Join(binDir, bin)); err != nil {
			t.Errorf("%s shim should survive opt-out: %v", bin, err)
		}
	}
}

// TestAddShellShims_OptOutWhenNoNodeShims is the fresh-install opt-out path
// (no prior managed node). os.Remove on a missing file must not surface as
// an error and must not block writing the rest of the shims.
func TestAddShellShims_OptOutWhenNoNodeShims(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("SHELL", "/bin/sh")

	binDir := filepath.Join(tmp, "lerd", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := addShellShims(false); err != nil {
		t.Fatalf("addShellShims(false) on clean bin dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(binDir, "php")); err != nil {
		t.Errorf("php shim should still be written: %v", err)
	}
}

// withSilencedStdout pipes os.Stdout to a discarded sink for the duration of
// f, so prompt prints don't pollute test output.
func withSilencedStdout(t *testing.T, f func()) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()
	defer func() {
		_ = w.Close()
		<-done
		os.Stdout = orig
	}()
	f()
}

func TestReadConfirmAnswer(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		defaultYes bool
		want       bool
	}{
		{"empty defaults yes", "\n", true, true},
		{"empty defaults no", "\n", false, false},
		{"eof defaults yes", "", true, true},
		{"eof defaults no", "", false, false},
		{"y", "y\n", false, true},
		{"yes", "yes\n", false, true},
		{"Y", "Y\n", false, true},
		{"n", "n\n", true, false},
		{"no", "no\n", true, false},
		{"NO", "NO\n", true, false},
		{"whitespace defaults yes", "  \n", true, true},
		{"random falls back to yes", "maybe\n", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			withSilencedStdout(t, func() {
				got = readConfirmAnswer(strings.NewReader(tc.input), "test?", tc.defaultYes)
			})
			if got != tc.want {
				t.Errorf("readConfirmAnswer(%q, default=%v) = %v, want %v",
					tc.input, tc.defaultYes, got, tc.want)
			}
		})
	}
}

// When stdin is a pipe (not a TTY) and /dev/tty isn't accessible either,
// promptSource must report no terminal so confirmInstallPromptDefault can
// fall back to the default rather than reading EOF from the pipe.
func TestPromptSource_PipedStdinFallsBackToTTY(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	src, closer, ok := promptSource()
	if closer != nil {
		_ = closer.Close()
	}

	// On a developer machine /dev/tty usually opens fine, so we accept either
	// outcome but assert the invariants: when ok is true, src must not be the
	// piped stdin (since stdin isn't a character device here).
	if ok && src == os.Stdin {
		t.Fatal("promptSource returned piped stdin as the prompt source")
	}
	if !ok && src != nil {
		t.Fatal("promptSource reported no source but returned non-nil reader")
	}
}

func TestParseDNSMode(t *testing.T) {
	cases := []struct {
		in          string
		wantEnabled bool
		wantOK      bool
	}{
		{"managed", true, true},
		{"enabled", true, true},
		{"test", true, true},
		{"  TEST  ", true, true},
		{"localhost", false, true},
		{"disabled", false, true},
		{"off", false, true},
		{"LocalHost", false, true},
		{"", false, false},
		{"bogus", false, false},
	}
	for _, c := range cases {
		gotEnabled, gotOK := parseDNSMode(c.in)
		if gotEnabled != c.wantEnabled || gotOK != c.wantOK {
			t.Errorf("parseDNSMode(%q) = (%v, %v), want (%v, %v)",
				c.in, gotEnabled, gotOK, c.wantEnabled, c.wantOK)
		}
	}
}

// TestMergeMigrationRestarts_keepsHealTornDownContainers guards the install
// regression where a run that triggered both the podman-upgrade heal and a
// network migration overwrote the heal-torn-down container list with the
// migration's, leaving those services stopped. The merge must keep both sets,
// de-duplicated.
func TestMergeMigrationRestarts_keepsHealTornDownContainers(t *testing.T) {
	healed := []string{"lerd-mysql", "lerd-redis"}
	recreated := []string{"lerd-redis", "lerd-postgres"}

	got := mergeMigrationRestarts(healed, recreated)

	has := func(s string) bool {
		for _, c := range got {
			if c == s {
				return true
			}
		}
		return false
	}
	for _, c := range []string{"lerd-mysql", "lerd-redis", "lerd-postgres"} {
		if !has(c) {
			t.Errorf("%s missing from merged restart set %v", c, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("expected the de-duplicated union of 3 containers, got %v", got)
	}
}
