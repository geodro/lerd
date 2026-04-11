package ui

import "testing"

func TestServiceRecentLogs_unknownUnit(t *testing.T) {
	// Should return empty or whitespace for a unit that doesn't exist,
	// never panic. This contract must hold after the platform split.
	result := serviceRecentLogs("lerd-nonexistent-unit-xyz")
	if len(result) > 200 {
		t.Errorf("expected short/empty result for unknown unit, got %d bytes", len(result))
	}
}

func TestIsContainerUnit_linux(t *testing.T) {
	// On Linux, all units are container-based
	if !isContainerUnit("lerd-nginx") {
		t.Error("expected isContainerUnit to return true on linux")
	}
	if !isContainerUnit("lerd-dns") {
		t.Error("expected isContainerUnit to return true for lerd-dns on linux")
	}
}
