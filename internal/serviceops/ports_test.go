package serviceops

import (
	"errors"
	"testing"

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

func TestErrPortInUseSentinel(t *testing.T) {
	err := errors.Join(ErrPortInUse)
	if !errors.Is(err, ErrPortInUse) {
		t.Error("ErrPortInUse not matchable via errors.Is")
	}
}
