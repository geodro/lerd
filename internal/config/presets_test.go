package config

import (
	"testing"
)

func TestListPresets_IncludesShippedPresets(t *testing.T) {
	presets, err := ListPresets()
	if err != nil {
		t.Fatalf("ListPresets() error = %v", err)
	}
	want := map[string]bool{
		"phpmyadmin":    false,
		"pgadmin":       false,
		"mongo":         false,
		"mongo-express": false,
		"stripe-mock":   false,
	}
	for _, p := range presets {
		if _, ok := want[p.Name]; ok {
			want[p.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("ListPresets() missing bundled preset %q", name)
		}
	}
}

func TestListPresets_SortedByName(t *testing.T) {
	presets, err := ListPresets()
	if err != nil {
		t.Fatalf("ListPresets() error = %v", err)
	}
	for i := 1; i < len(presets); i++ {
		if presets[i-1].Name > presets[i].Name {
			t.Errorf("ListPresets() not sorted: %q > %q", presets[i-1].Name, presets[i].Name)
		}
	}
}

func TestLoadPreset_PhpMyAdmin(t *testing.T) {
	svc, err := LoadPreset("phpmyadmin")
	if err != nil {
		t.Fatalf("LoadPreset(phpmyadmin) error = %v", err)
	}
	if svc.Name != "phpmyadmin" {
		t.Errorf("Name = %q, want phpmyadmin", svc.Name)
	}
	if svc.Image == "" {
		t.Errorf("Image is empty")
	}
	if len(svc.Ports) == 0 {
		t.Errorf("Ports is empty")
	}
	foundDep := false
	for _, d := range svc.DependsOn {
		if d == "mysql" {
			foundDep = true
		}
	}
	if !foundDep {
		t.Errorf("phpmyadmin should depend on mysql, got %v", svc.DependsOn)
	}
	if svc.Dashboard == "" {
		t.Errorf("Dashboard is empty")
	}
}

func TestLoadPreset_PgAdmin(t *testing.T) {
	svc, err := LoadPreset("pgadmin")
	if err != nil {
		t.Fatalf("LoadPreset(pgadmin) error = %v", err)
	}
	if svc.Name != "pgadmin" {
		t.Errorf("Name = %q, want pgadmin", svc.Name)
	}
	foundDep := false
	for _, d := range svc.DependsOn {
		if d == "postgres" {
			foundDep = true
		}
	}
	if !foundDep {
		t.Errorf("pgadmin should depend on postgres, got %v", svc.DependsOn)
	}
}

func TestLoadPreset_Unknown(t *testing.T) {
	if _, err := LoadPreset("does-not-exist"); err == nil {
		t.Errorf("LoadPreset(does-not-exist) expected error, got nil")
	}
}

func TestPresetExists(t *testing.T) {
	if !PresetExists("phpmyadmin") {
		t.Errorf("PresetExists(phpmyadmin) = false, want true")
	}
	if !PresetExists("pgadmin") {
		t.Errorf("PresetExists(pgadmin) = false, want true")
	}
	if PresetExists("nope") {
		t.Errorf("PresetExists(nope) = true, want false")
	}
}
