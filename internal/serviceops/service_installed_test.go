package serviceops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestServiceInstalled_defaultPresetQuadletIsTruth: built-in default presets
// have no services/ YAML, so their quadlet stays the install-state signal even
// with no YAML present.
func TestServiceInstalled_defaultPresetQuadletIsTruth(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	if ServiceInstalled("mysql") {
		t.Fatalf("expected mysql not installed on a clean tree")
	}

	quadletDir := config.QuadletDir()
	if err := os.MkdirAll(quadletDir, 0o755); err != nil {
		t.Fatalf("mkdir quadlet dir: %v", err)
	}
	quadletPath := filepath.Join(quadletDir, "lerd-mysql.container")
	if err := os.WriteFile(quadletPath, []byte("[Container]\nImage=docker.io/library/mysql:8.4\n"), 0o644); err != nil {
		t.Fatalf("write quadlet: %v", err)
	}

	if !ServiceInstalled("mysql") {
		t.Fatalf("expected mysql installed when quadlet exists, even with no YAML")
	}

	if _, err := config.LoadCustomService("mysql"); err == nil {
		t.Fatalf("test precondition broken: YAML should not exist for this scenario")
	}
}

// TestServiceInstalled_customServiceYAMLIsTruth covers the orphan-quadlet bug
// (issue #678): for a custom service the YAML is the truth, so a leftover
// quadlet with no backing YAML must report NOT installed.
func TestServiceInstalled_customServiceYAMLIsTruth(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))

	if config.IsDefaultPreset("gotenberg") {
		t.Fatalf("test premise broken: gotenberg must be a non-default preset")
	}

	if ServiceInstalled("gotenberg") {
		t.Fatalf("expected gotenberg not installed on a clean tree")
	}

	// Orphan quadlet with no YAML must NOT count as installed.
	quadletDir := config.QuadletDir()
	if err := os.MkdirAll(quadletDir, 0o755); err != nil {
		t.Fatalf("mkdir quadlet dir: %v", err)
	}
	quadletPath := filepath.Join(quadletDir, "lerd-gotenberg.container")
	if err := os.WriteFile(quadletPath, []byte("[Container]\nImage=docker.io/gotenberg/gotenberg:8\n"), 0o644); err != nil {
		t.Fatalf("write quadlet: %v", err)
	}
	if ServiceInstalled("gotenberg") {
		t.Fatalf("orphan quadlet with no YAML must report NOT installed for a custom service")
	}

	// With the YAML present it is installed.
	if err := config.SaveCustomService(&config.CustomService{Name: "gotenberg", Image: "docker.io/gotenberg/gotenberg:8"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !ServiceInstalled("gotenberg") {
		t.Fatalf("expected gotenberg installed once its YAML exists")
	}
}
