// Package activityping notifies a running lerd-ui that a site is being worked
// on, so out-of-process tools (the CLI shims, the MCP server) keep a site awake
// under idle-suspend the same way an HTTP request would, and wake it if it was
// asleep. Best-effort and fast: it pings lerd-ui's loopback unix socket with a
// tight timeout and ignores every failure (lerd-ui not running, etc.).
package activityping

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/geodro/lerd/internal/config"
)

// client talks to lerd-ui over its unix socket with a short timeout so a caller
// is never held up by the ping (a missing lerd-ui fails the dial almost
// instantly).
var client = &http.Client{
	Timeout: 300 * time.Millisecond,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, "unix", config.UISocketPath())
		},
	},
}

// Site records activity for the named site. No-op on an empty name.
func Site(name string) {
	if name == "" {
		return
	}
	req, err := http.NewRequest(http.MethodPost, "http://lerd/api/internal/activity?site="+url.QueryEscape(name), nil)
	if err != nil {
		return
	}
	if resp, err := client.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}
