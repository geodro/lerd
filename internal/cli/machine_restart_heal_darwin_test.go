//go:build darwin

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShouldHealAfterMachineStart(t *testing.T) {
	cases := []struct {
		name            string
		preEnsureLastUp string
		priorBaseline   string
		want            bool
	}{
		{"first run, no baseline yet", "2026-05-27T12:00:00Z", "", false},
		{"first run, machine was down", "", "", false},
		{"unchanged across runs", "2026-05-26T12:00:00Z", "2026-05-26T12:00:00Z", false},
		{"external restart between runs", "2026-05-27T08:30:00Z", "2026-05-26T12:00:00Z", true},
		{"machine was down, baseline exists", "", "2026-05-26T12:00:00Z", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldHealAfterMachineStart(c.preEnsureLastUp, c.priorBaseline); got != c.want {
				t.Errorf("shouldHealAfterMachineStart(%q, %q) = %v, want %v",
					c.preEnsureLastUp, c.priorBaseline, got, c.want)
			}
		})
	}
}

func TestReadPriorMachineLastUp_missing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if got := readPriorMachineLastUp(); got != "" {
		t.Errorf("expected empty when state file missing, got %q", got)
	}
}

func TestReadPriorMachineLastUp_roundTrip(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(machineLastUpFile()), 0755); err != nil {
		t.Fatal(err)
	}
	want := "2026-05-26T15:32:41.93164+03:00"
	if err := os.WriteFile(machineLastUpFile(), []byte(want+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := readPriorMachineLastUp(); got != want {
		t.Errorf("readPriorMachineLastUp() = %q, want %q (trailing whitespace must be trimmed)", got, want)
	}
}
