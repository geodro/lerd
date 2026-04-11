package cli

import (
	"strings"
	"testing"
)

func TestServiceStartHint_containsUnit(t *testing.T) {
	hint := serviceStartHint("lerd-nginx")
	if !strings.Contains(hint, "lerd-nginx") && !strings.Contains(hint, "lerd start") {
		t.Errorf("serviceStartHint should reference the unit or lerd start, got %q", hint)
	}
}

func TestServiceStatusHint_containsUnit(t *testing.T) {
	hint := serviceStatusHint("lerd-nginx")
	if !strings.Contains(hint, "lerd-nginx") && !strings.Contains(hint, "lerd start") {
		t.Errorf("serviceStatusHint should reference the unit or lerd start, got %q", hint)
	}
}

func TestDnsRestartHint_nonEmpty(t *testing.T) {
	hint := dnsRestartHint()
	if hint == "" {
		t.Error("dnsRestartHint should not be empty")
	}
}

func TestPodmanDaemonHint_nonEmpty(t *testing.T) {
	hint := podmanDaemonHint()
	if hint == "" {
		t.Error("podmanDaemonHint should not be empty")
	}
}
