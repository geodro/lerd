package cli

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestWorkerStartForSite_skipsLifecycleWhenUnsupported pins the contract
// fixed in this round: when workerSupportedOnPlatform reports the worker
// can't run on this platform, WorkerStartForSite must return nil without
// calling Enable/StartUnit. On macOS the prior behaviour was to print a
// WARN, return (false, nil) from writeWorkerUnitFile, then proceed to
// StartUnit on a non-existent unit — producing a confusing podman error
// after the WARN.
func TestWorkerStartForSite_skipsLifecycleWhenUnsupported(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	fake := &stopTrackingMgr{}
	swapMgr(t, fake)

	prev := workerSupportedOnPlatform
	workerSupportedOnPlatform = func(_ config.FrameworkWorker) (bool, string) {
		return false, "host: true workers aren't supported on macOS yet"
	}
	t.Cleanup(func() { workerSupportedOnPlatform = prev })

	w := config.FrameworkWorker{Command: "npm run dev", Host: true}
	if err := WorkerStartForSite("ws", "/p/ws", "8.4", "vite", w, false); err != nil {
		t.Fatalf("expected nil error for unsupported worker, got %v", err)
	}

	if len(fake.disableCalls)+len(fake.removeServiceCalls)+len(fake.removeTimerCalls) != 0 {
		t.Errorf("expected zero lifecycle calls, got disable=%v remove=%v removeTimer=%v",
			fake.disableCalls, fake.removeServiceCalls, fake.removeTimerCalls)
	}
}

// TestWorkerStartForSite_unsupportedReasonPrinted ensures the WARN line
// surfaces the reason verbatim so users can tell *why* a worker was
// silently skipped.
func TestWorkerStartForSite_unsupportedReasonPrinted(t *testing.T) {
	registerSite(t, "ws", "/p/ws")
	swapMgr(t, &stopTrackingMgr{})

	prev := workerSupportedOnPlatform
	workerSupportedOnPlatform = func(_ config.FrameworkWorker) (bool, string) {
		return false, "fake-platform-reason"
	}
	t.Cleanup(func() { workerSupportedOnPlatform = prev })

	out := captureStdout(t, func() {
		_ = WorkerStartForSite("ws", "/p/ws", "8.4", "vite", config.FrameworkWorker{Command: "x"}, false)
	})
	if !strings.Contains(out, "fake-platform-reason") {
		t.Errorf("expected reason in WARN output, got %q", out)
	}
	if !strings.Contains(out, "[WARN]") {
		t.Errorf("expected [WARN] marker in output, got %q", out)
	}
}
