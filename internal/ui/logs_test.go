package ui

import (
	"runtime"
	"testing"
)

func TestServiceRecentLogs_unknownUnit(t *testing.T) {
	result := serviceRecentLogs("lerd-nonexistent-unit-xyz")
	if len(result) > 200 {
		t.Errorf("expected short/empty result for unknown unit, got %d bytes", len(result))
	}
}

func TestIsContainerUnit_nginx(t *testing.T) {
	// lerd-nginx is a container on both platforms
	if !isContainerUnit("lerd-nginx") {
		t.Error("expected isContainerUnit to return true for lerd-nginx")
	}
}

func TestIsContainerUnit_dns(t *testing.T) {
	if runtime.GOOS == "darwin" {
		// On macOS, lerd-dns runs natively via Homebrew
		if isContainerUnit("lerd-dns") {
			t.Error("expected isContainerUnit to return false for lerd-dns on macOS")
		}
	} else {
		// On Linux, lerd-dns is a container
		if !isContainerUnit("lerd-dns") {
			t.Error("expected isContainerUnit to return true for lerd-dns on linux")
		}
	}
}
