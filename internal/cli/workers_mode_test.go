package cli

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// Scope: workersModeFromArgs is the pure decision layer of the
// `lerd workers mode` command. It takes the user's args and the current
// config value and returns either (newMode, nil) to apply or ("", err)
// to surface. Keeping the I/O (config load/save, stdout) in the cobra
// RunE keeps this part trivially testable.

func TestWorkersModeFromArgs_NoArgsReturnsShow(t *testing.T) {
	mode, show, err := workersModeFromArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !show {
		t.Error("no args should request show-current behaviour")
	}
	if mode != "" {
		t.Errorf("no args should not propose a mode, got %q", mode)
	}
}

func TestWorkersModeFromArgs_KnownValue(t *testing.T) {
	for _, m := range []string{config.WorkerExecModeExec, config.WorkerExecModeContainer} {
		mode, show, err := workersModeFromArgs([]string{m})
		if err != nil {
			t.Errorf("%q: unexpected error %v", m, err)
		}
		if show {
			t.Errorf("%q: show should be false when setting", m)
		}
		if mode != m {
			t.Errorf("%q: got mode %q", m, mode)
		}
	}
}

func TestWorkersModeFromArgs_UnknownValueErrors(t *testing.T) {
	_, _, err := workersModeFromArgs([]string{"garbage"})
	if err == nil {
		t.Error("unknown mode should error")
	}
}

func TestApplyWorkersMode_UpdatesConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	if err := applyWorkersMode(config.WorkerExecModeContainer); err != nil {
		t.Fatalf("applyWorkersMode: %v", err)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := cfg.WorkerExecMode(); got != config.WorkerExecModeContainer {
		t.Errorf("config not updated: got %q", got)
	}

	// Flip back to exec to ensure both directions work.
	if err := applyWorkersMode(config.WorkerExecModeExec); err != nil {
		t.Fatalf("apply exec: %v", err)
	}
	cfg2, _ := config.LoadGlobal()
	if got := cfg2.WorkerExecMode(); got != config.WorkerExecModeExec {
		t.Errorf("flip back failed: got %q", got)
	}
}

func TestApplyWorkersMode_SameValueIsNoOp(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	// Default is exec; applying exec should succeed without error.
	if err := applyWorkersMode(config.WorkerExecModeExec); err != nil {
		t.Fatalf("applyWorkersMode no-op: %v", err)
	}
}
