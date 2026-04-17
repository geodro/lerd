package ui

import (
	"context"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
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

// streamUnitLogs must send the response headers and an initial SSE comment
// before running the log-following subprocess; otherwise silent units leave
// the browser's EventSource stuck in CONNECTING and the UI sticks on
// "connecting...".
func TestStreamUnitLogs_flushesHeadersBeforeStreaming(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req := httptest.NewRequest("GET", "/api/schedule/nonexistent/logs", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		streamUnitLogs(rec, req, "lerd-nonexistent-unit-xyz")
		close(done)
	}()
	<-done

	if ct := rec.Result().Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, ": connected") {
		n := len(body)
		if n > 40 {
			n = 40
		}
		t.Errorf("body should start with initial SSE comment, got: %q", body[:n])
	}
}
