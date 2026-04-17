package cli

import (
	"testing"
)

// TestXdebugOnCmd_ModeFlag guards the --mode flag wiring against accidental
// removal during future refactors. Without the flag users can't reach any
// mode other than the default "debug", which defeats the purpose of the
// per-mode support.
func TestXdebugOnCmd_ModeFlag(t *testing.T) {
	cmd := newXdebugOnCmd()
	flag := cmd.Flags().Lookup("mode")
	if flag == nil {
		t.Fatal("--mode flag not registered on `lerd xdebug on`")
	}
	if flag.DefValue != "debug" {
		t.Errorf("--mode default = %q, want %q", flag.DefValue, "debug")
	}
}
