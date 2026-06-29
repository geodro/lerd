package cli

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// withTempXDG isolates the test from the developer's real lerd state.
func withTempXDG(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
}

type noopLifecycle struct{}

func (noopLifecycle) Start(string) error                { return nil }
func (noopLifecycle) Stop(string) error                 { return nil }
func (noopLifecycle) Restart(string) error              { return nil }
func (noopLifecycle) UnitStatus(string) (string, error) { return "active", nil }
func (noopLifecycle) AllUnitStates() map[string]string  { return map[string]string{} }

func stubPodman(t *testing.T) {
	t.Helper()
	prevLC := podman.UnitLifecycle
	prevReload := podman.DaemonReloadFn
	podman.UnitLifecycle = noopLifecycle{}
	podman.DaemonReloadFn = func() error { return nil }
	t.Cleanup(func() {
		podman.UnitLifecycle = prevLC
		podman.DaemonReloadFn = prevReload
	})
}

func TestRunDumpToggle_OnEnablesConfigEvenWithoutVersions(t *testing.T) {
	withTempXDG(t)
	stubPodman(t)

	if err := runDumpToggle(true); err != nil {
		t.Fatalf("runDumpToggle on: %v", err)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsDumpsEnabled() {
		t.Errorf("Dumps.Enabled not persisted")
	}
}

func TestRunDumpToggle_NoChangeOnSecondCall(t *testing.T) {
	withTempXDG(t)
	stubPodman(t)

	if err := runDumpToggle(true); err != nil {
		t.Fatal(err)
	}
	// Second call should be NoChange — no error, config unchanged.
	if err := runDumpToggle(true); err != nil {
		t.Fatalf("second runDumpToggle: %v", err)
	}
}

func TestRunDumpToggle_OffRoundTrip(t *testing.T) {
	withTempXDG(t)
	stubPodman(t)

	_ = runDumpToggle(true)
	if err := runDumpToggle(false); err != nil {
		t.Fatalf("runDumpToggle off: %v", err)
	}
	cfg, _ := config.LoadGlobal()
	if cfg.IsDumpsEnabled() {
		t.Errorf("Dumps.Enabled still true after off")
	}
}

// withShortDataDir reroutes XDG_DATA_HOME to a tmp dir with a minimal
// prefix so paths under DataDir() (including the UI socket) fit within
// macOS's 104-byte sockaddr_un.sun_path limit. t.TempDir() under
// /var/folders/.../T/<long-test-name>/data/... routinely overflows.
func withShortDataDir(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ld-")
	if err != nil {
		t.Fatalf("mkdir short data dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_DATA_HOME", dir)
}

// fakeUISocket binds a HTTP server on config.UISocketPath() and records
// every path it receives. Returns a pointer the test can check. Requires
// withShortDataDir to have set XDG_DATA_HOME to a short path so the
// resulting socket path fits within the macOS sun_path limit.
func fakeUISocket(t *testing.T) *atomic.Pointer[string] {
	t.Helper()
	sockPath := config.UISocketPath()
	if err := os.MkdirAll(filepath.Dir(sockPath), 0755); err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	// Point the CLI client at this unix socket regardless of the production
	// per-OS default (TCP loopback on macOS), so the test exercises the same
	// handler on both platforms.
	prevDial := uiClientDial
	uiClientDial = func() (string, string) { return "unix", sockPath }
	t.Cleanup(func() { uiClientDial = prevDial })
	var seen atomic.Pointer[string]
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		seen.Store(&p)
		w.WriteHeader(http.StatusNoContent)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
	return &seen
}

func TestRunDumpToggle_PingsUIAfterChange(t *testing.T) {
	withTempXDG(t)
	withShortDataDir(t)
	stubPodman(t)
	seen := fakeUISocket(t)

	if err := runDumpToggle(true); err != nil {
		t.Fatalf("runDumpToggle: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p := seen.Load(); p != nil && *p == "/api/dumps/notify-changed" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := seen.Load()
	gotStr := "<nil>"
	if got != nil {
		gotStr = *got
	}
	t.Fatalf("did not see notify-changed ping on UI socket, last path=%s", gotStr)
}

func TestRunDumpToggle_NoPingWhenNoChange(t *testing.T) {
	withTempXDG(t)
	withShortDataDir(t)
	stubPodman(t)
	// Enable once (will ping).
	_ = runDumpToggle(true)

	seen := fakeUISocket(t)
	// Calling on() again is NoChange; no ping should fire.
	if err := runDumpToggle(true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if p := seen.Load(); p != nil {
		t.Errorf("expected no ping on no-change toggle, got %q", *p)
	}
}

func TestNewDumpCmd_HasExpectedSubcommands(t *testing.T) {
	cmd := NewDumpCmd()
	want := []string{"on", "off", "status", "tail", "clear"}
	have := map[string]bool{}
	for _, c := range cmd.Commands() {
		have[c.Name()] = true
	}
	missing := []string{}
	for _, w := range want {
		if !have[w] {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		t.Errorf("missing subcommand(s) %s", strings.Join(missing, ", "))
	}
}
