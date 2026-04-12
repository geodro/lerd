package cli

import (
	"strings"
	"testing"
)

func TestPortInUseIn_match(t *testing.T) {
	output := "LISTEN  0  128  127.0.0.1:5300  0.0.0.0:*"
	if !portInUseIn("5300", output) {
		t.Error("expected port 5300 to be found in output")
	}
}

func TestPortInUseIn_noMatch(t *testing.T) {
	output := "LISTEN  0  128  127.0.0.1:5300  0.0.0.0:*"
	if portInUseIn("80", output) {
		t.Error("expected port 80 not to be found")
	}
}

func TestPortInUseIn_emptyOutput(t *testing.T) {
	if portInUseIn("80", "") {
		t.Error("expected false for empty output")
	}
}

func TestPortInUseIn_partialPortMatch(t *testing.T) {
	output := "LISTEN  0  128  127.0.0.1:8080  0.0.0.0:*"
	if portInUseIn("80", output) {
		t.Error("port 80 should not match :8080")
	}
}

func TestPortInUse_unusedPort(t *testing.T) {
	if portInUse("59999") {
		t.Error("port 59999 should not be in use")
	}
}

func TestPortListOutput_format(t *testing.T) {
	output := portListOutput()
	if output != "" && !strings.Contains(output, "LISTEN") && !strings.Contains(output, "State") {
		t.Errorf("portListOutput should contain LISTEN headers, got: %.100s", output)
	}
}
