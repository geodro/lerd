package serviceops

import (
	"errors"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
)

func TestValidateExtraPort(t *testing.T) {
	cases := []struct {
		spec string
		ok   bool
	}{
		{"3411", true},
		{"3411:3306", true},
		{"127.0.0.1:3411:3306", true},
		{"3411:3306/tcp", true},
		{"53:53/udp", true},
		{"", false},
		{"nope", false},
		{"3411:bad", false},
		{"70000:3306", false},
		{"-1:3306", false},
		{"a:b:c:d", false},
	}
	for _, c := range cases {
		err := ValidateExtraPort(c.spec)
		if c.ok && err != nil {
			t.Errorf("ValidateExtraPort(%q) = %v, want nil", c.spec, err)
		}
		if !c.ok && err == nil {
			t.Errorf("ValidateExtraPort(%q) = nil, want error", c.spec)
		}
	}
}

func TestRemovePort(t *testing.T) {
	got := removePort([]string{"3411:3306", "39580:80", "3411:3306"}, "3411:3306")
	if len(got) != 1 || got[0] != "39580:80" {
		t.Errorf("removePort dropped wrong entries: %v", got)
	}
}

// TestSetPublishedPortRange rejects out-of-range ports before touching config.
func TestSetPublishedPortRange(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if _, err := SetPublishedPort("mysql", 70000); err == nil {
		t.Fatal("SetPublishedPort(70000) = nil, want range error")
	}
}

// TestSetPublishedPortNotInstalled saves the override for a built-in service that
// isn't installed and never resurrects the unit.
func TestSetPublishedPortNotInstalled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	res, err := SetPublishedPort("mysql", 33991)
	if err != nil {
		t.Fatalf("SetPublishedPort: %v", err)
	}
	if res.Installed {
		t.Error("Installed = true for an uninstalled service")
	}
	if res.Actual != 33991 {
		t.Errorf("Actual = %d, want 33991", res.Actual)
	}
	if config.ServicePublishedPort("mysql") != 33991 {
		t.Errorf("override not persisted, got %d", config.ServicePublishedPort("mysql"))
	}
}

// TestSetPublishedPortNoOp reports NoOp when the requested port already matches
// the saved override and doesn't error.
func TestSetPublishedPortNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if _, err := SetPublishedPort("mysql", 33991); err != nil {
		t.Fatalf("first SetPublishedPort: %v", err)
	}
	res, err := SetPublishedPort("mysql", 33991)
	if err != nil {
		t.Fatalf("second SetPublishedPort: %v", err)
	}
	if !res.NoOp {
		t.Error("NoOp = false, want true on repeat of the same port")
	}
}

// TestSetExtraPortsDedup persists a de-duplicated, validated set for a built-in
// service and rejects a malformed mapping.
func TestSetExtraPortsDedup(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if err := SetExtraPorts("mysql", []string{"39580:80", "39580:80", " "}); err != nil {
		t.Fatalf("SetExtraPorts: %v", err)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Services["mysql"].ExtraPorts
	if len(got) != 1 || got[0] != "39580:80" {
		t.Errorf("ExtraPorts = %v, want [39580:80]", got)
	}
	if err := SetExtraPorts("mysql", []string{"bad"}); err == nil {
		t.Error("SetExtraPorts(bad) = nil, want validation error")
	}
}

// TestAddRemoveExtraPort adds then removes a single mapping for a built-in service.
func TestAddRemoveExtraPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if err := AddExtraPort("mysql", "39590:9000"); err != nil {
		t.Fatalf("AddExtraPort: %v", err)
	}
	if cfg, _ := config.LoadGlobal(); len(cfg.Services["mysql"].ExtraPorts) != 1 {
		t.Fatalf("AddExtraPort did not persist: %v", cfg.Services["mysql"].ExtraPorts)
	}
	if err := RemoveExtraPort("mysql", "39590:9000"); err != nil {
		t.Fatalf("RemoveExtraPort: %v", err)
	}
	if cfg, _ := config.LoadGlobal(); len(cfg.Services["mysql"].ExtraPorts) != 0 {
		t.Errorf("RemoveExtraPort left entries: %v", cfg.Services["mysql"].ExtraPorts)
	}
}

func TestSetExtraPortsRejectsCustomName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := SetExtraPorts("not-a-preset", []string{"39580:80"}); err == nil {
		t.Error("SetExtraPorts on a non-preset = nil, want error")
	}
}

// TestSetExtraPortsOptionalPreset persists extra ports for an optional (non
// default-stack) preset like gotenberg: it's a service we ship, so the gate is
// preset ownership, not the default flag.
func TestSetExtraPortsOptionalPreset(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := SetExtraPorts("gotenberg", []string{"39580:80"}); err != nil {
		t.Fatalf("SetExtraPorts(gotenberg): %v", err)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services["gotenberg"].ExtraPorts; len(got) != 1 || got[0] != "39580:80" {
		t.Errorf("gotenberg ExtraPorts = %v, want [39580:80]", got)
	}
}

// TestSetPublishedPortRejectsSiblingPort refuses a port another lerd service
// already claims (postgres's default 5432) even while that sibling is stopped.
func TestSetPublishedPortRejectsSiblingPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	_, err := SetPublishedPort("mysql", 5432)
	if !errors.Is(err, ErrPortReserved) {
		t.Fatalf("SetPublishedPort(mysql, 5432) err = %v, want ErrPortReserved", err)
	}
}

// TestSetPublishedPortRejectsOwnExtraPort refuses a published port that already
// serves as one of the same service's extra ports.
func TestSetPublishedPortRejectsOwnExtraPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := SetExtraPorts("mysql", []string{"33950:3306"}); err != nil {
		t.Fatal(err)
	}
	_, err := SetPublishedPort("mysql", 33950)
	if !errors.Is(err, ErrPortInUse) {
		t.Fatalf("SetPublishedPort onto own extra port err = %v, want ErrPortInUse", err)
	}
}

// TestSetExtraPortsRejectsMainPort refuses an extra mapping that republishes the
// service's own main host port.
func TestSetExtraPortsRejectsMainPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := SetExtraPorts("mysql", []string{"3306:3306"}); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("SetExtraPorts onto main port err = %v, want ErrPortInUse", err)
	}
}

// TestSetExtraPortsRejectsSiblingPort refuses an extra mapping on a host port
// another lerd service already claims.
func TestSetExtraPortsRejectsSiblingPort(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := SetExtraPorts("mysql", []string{"5432:5432"}); !errors.Is(err, ErrPortReserved) {
		t.Fatalf("SetExtraPorts onto postgres's port err = %v, want ErrPortReserved", err)
	}
}

// TestSetPublishedPortDefaultResetsNotCollides pins finding #4: asking for the
// preset default normalises to a reset (override 0) instead of erroring as
// "port already in use" — a running service holds its own default port, so the
// old bind probe rejected `lerd service port mysql 3306` while mysql owned 3306.
func TestSetPublishedPortDefaultResetsNotCollides(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if _, err := SetPublishedPort("mysql", 33071); err != nil {
		t.Fatalf("move off default: %v", err)
	}
	res, err := SetPublishedPort("mysql", 3306) // mysql's preset default
	if err != nil {
		t.Fatalf("requesting the preset default must not error, got %v", err)
	}
	if got := config.ServicePublishedPort("mysql"); got != 0 {
		t.Errorf("requesting the default must reset the override to 0, got %d", got)
	}
	_ = res
}

// TestSetPublishedPortDefaultNoOpWhenAlreadyDefault: a service already on its
// default reports NoOp when asked for that same default port, never an error.
func TestSetPublishedPortDefaultNoOpWhenAlreadyDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	res, err := SetPublishedPort("mysql", 3306)
	if err != nil {
		t.Fatalf("requesting the default on a default service must not error, got %v", err)
	}
	if !res.NoOp {
		t.Error("requesting the current default must be a NoOp")
	}
}

// TestSetPublishedPortRollsBackOnStartFailure pins finding #3: when the restart
// on the new port fails, the service is brought back up on its previous port and
// the config is rolled back, instead of left down with the override already moved.
func TestSetPublishedPortRollsBackOnStartFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	fakeQuadletOnDisk(t, "mysql") // ServiceInstalled -> true

	prevStatus, prevStop, prevStart := portsUnitStatus, portsStopUnit, portsStartUnit
	prevWait, prevRerender := portsWaitReady, portsRerender
	t.Cleanup(func() {
		portsUnitStatus, portsStopUnit, portsStartUnit = prevStatus, prevStop, prevStart
		portsWaitReady, portsRerender = prevWait, prevRerender
	})

	startCalls := 0
	portsUnitStatus = func(string) (string, error) { return "active", nil }
	portsStopUnit = func(string) error { return nil }
	portsRerender = func(string) error { return nil }
	portsWaitReady = func(string, time.Duration) error { return nil }
	portsStartUnit = func(string) error {
		startCalls++
		if startCalls == 1 {
			return errors.New("address already in use")
		}
		return nil // the rollback restart on the previous port succeeds
	}

	if _, err := SetPublishedPort("mysql", 33072); err == nil {
		t.Fatal("a failed start must surface an error so the caller knows the change didn't take")
	}
	if startCalls != 2 {
		t.Fatalf("expected a rollback restart attempt after the failed start, startCalls=%d", startCalls)
	}
	if got := config.ServicePublishedPort("mysql"); got != 0 {
		t.Errorf("config must roll back to the previous port (0=default), got %d", got)
	}
}

func TestErrPortInUseSentinel(t *testing.T) {
	err := errors.Join(ErrPortInUse)
	if !errors.Is(err, ErrPortInUse) {
		t.Error("ErrPortInUse not matchable via errors.Is")
	}
}
