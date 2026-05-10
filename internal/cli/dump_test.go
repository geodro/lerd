package cli

import (
	"path/filepath"
	"strings"
	"testing"

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
