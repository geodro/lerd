package ui

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestHostProxyAppLifecycleOp pins the routing that fixes the host-proxy 502:
// stopping/starting the parent "app" worker must become a site pause/unpause
// (which swaps the proxy vhost), while every other case keeps the plain
// per-worker path.
func TestHostProxyAppLifecycleOp(t *testing.T) {
	app := config.HostProxyWorkerName
	cases := []struct {
		name               string
		isHostProxy        bool
		worker, branch, op string
		wantOp             string
		wantOK             bool
	}{
		{"host-proxy app stop -> pause", true, app, "", "stop", "pause", true},
		{"host-proxy app start -> unpause", true, app, "", "start", "unpause", true},
		{"non-host-proxy app untouched", false, app, "", "stop", "", false},
		{"other worker on host-proxy untouched", true, "queue", "", "stop", "", false},
		{"worktree app untouched", true, app, "feature", "stop", "", false},
		{"unknown op untouched", true, app, "", "restart", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotOp, gotOK := hostProxyAppLifecycleOp(tc.isHostProxy, tc.worker, tc.branch, tc.op)
			if gotOp != tc.wantOp || gotOK != tc.wantOK {
				t.Fatalf("hostProxyAppLifecycleOp(%v, %q, %q, %q) = (%q, %v), want (%q, %v)",
					tc.isHostProxy, tc.worker, tc.branch, tc.op, gotOp, gotOK, tc.wantOp, tc.wantOK)
			}
		})
	}
}
