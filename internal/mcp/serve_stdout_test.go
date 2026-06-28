package mcp

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
)

// TestGuardStdout_divertsStrayWritesOffTheProtocolStream proves the Serve-time
// stdout guard: the JSON-RPC encoder keeps writing to the real stdout, while any
// in-process diagnostic that still targets os.Stdout is diverted to stderr so it
// cannot corrupt a protocol frame.
func TestGuardStdout_divertsStrayWritesOffTheProtocolStream(t *testing.T) {
	protoR, protoW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = protoW, errW
	t.Cleanup(func() { os.Stdout, os.Stderr = savedOut, savedErr })

	real, restore := guardStdout()
	fmt.Fprint(os.Stdout, "stray diagnostic") // a handler still printing to stdout
	fmt.Fprint(real, `{"jsonrpc":"2.0"}`)     // the encoder, on the real stdout
	restore()

	_ = protoW.Close()
	_ = errW.Close()
	var proto, diag bytes.Buffer
	_, _ = io.Copy(&proto, protoR)
	_, _ = io.Copy(&diag, errR)

	if proto.String() != `{"jsonrpc":"2.0"}` {
		t.Errorf("protocol stream = %q, want only the JSON-RPC frame (no stray diagnostics)", proto.String())
	}
	if diag.String() != "stray diagnostic" {
		t.Errorf("stray stdout write landed on %q, want it diverted to stderr as %q", diag.String(), "stray diagnostic")
	}

	if os.Stdout != real {
		t.Error("restore must put os.Stdout back to what it was when guardStdout was called")
	}
}
