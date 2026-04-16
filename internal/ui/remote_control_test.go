package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// setupConfigDir points config.LoadGlobal at a temp dir, optionally writing
// a config.yaml with the given UI credentials. When credentials are
// provided, lan.exposed is also set to true: the gate now treats LAN
// exposure as a top-level flag, so credentials without lan:expose result
// in 403 (which is correct production behavior but would break every
// existing "non-loopback with valid auth → 200" test). Tests that
// specifically want to verify the LAN-off-with-creds path should call
// setupConfigDirRaw directly.
func setupConfigDir(t *testing.T, username, plainPassword string) {
	t.Helper()
	setupConfigDirRaw(t, username, plainPassword, username != "" || plainPassword != "")
}

func setupConfigDirRaw(t *testing.T, username, plainPassword string, lanExposed bool) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg := map[string]any{}
	if lanExposed {
		cfg["lan"] = map[string]any{"exposed": true}
	}
	if username != "" || plainPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("bcrypt: %v", err)
		}
		cfg["ui"] = map[string]any{
			"username":      username,
			"password_hash": string(hash),
		}
	}
	if len(cfg) == 0 {
		return
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	dir := filepath.Join(tmp, "lerd")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// nextHandler is a stub downstream handler that records whether it was
// called and writes a 200 OK with a marker body.
type nextHandler struct {
	called bool
}

func (n *nextHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.called = true
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func TestRemoteControlGate_loopbackBypassesEverything(t *testing.T) {
	setupConfigDir(t, "alice", "s3cret")

	next := &nextHandler{}
	gate := withRemoteControlGate(next)

	req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if !next.called {
		t.Error("loopback request did not reach next handler")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("loopback status = %d, want 200", rec.Code)
	}
}

func TestRemoteControlGate_lanForbiddenWhenDisabled(t *testing.T) {
	setupConfigDir(t, "", "") // no auth configured

	next := &nextHandler{}
	gate := withRemoteControlGate(next)

	req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	req.RemoteAddr = "192.168.1.42:54321"
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if next.called {
		t.Error("LAN request reached next handler when remote-control is off")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("LAN status = %d, want 403", rec.Code)
	}
}

func TestRemoteControlGate_lanRequiresAuthWhenEnabled(t *testing.T) {
	setupConfigDir(t, "alice", "s3cret")

	next := &nextHandler{}
	gate := withRemoteControlGate(next)

	t.Run("no header → 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
		req.RemoteAddr = "192.168.1.42:54321"
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Error("missing WWW-Authenticate header")
		}
	})

	t.Run("wrong user → 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
		req.RemoteAddr = "192.168.1.42:54321"
		req.SetBasicAuth("bob", "s3cret")
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("wrong password → 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
		req.RemoteAddr = "192.168.1.42:54321"
		req.SetBasicAuth("alice", "wrong")
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("correct creds → 200", func(t *testing.T) {
		next2 := &nextHandler{}
		gate2 := withRemoteControlGate(next2)
		req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
		req.RemoteAddr = "192.168.1.42:54321"
		req.SetBasicAuth("alice", "s3cret")
		rec := httptest.NewRecorder()
		gate2.ServeHTTP(rec, req)
		if !next2.called {
			t.Error("authorized LAN request did not reach next handler")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
}

// TestRemoteControlGate_lanOffOverridesCredentials verifies the top-level
// LAN-exposure gate: when cfg.LAN.Exposed is false, LAN clients are denied
// even if they present valid Basic auth credentials. This catches the
// regression where stale credentials from a prior `lerd remote-control on`
// session would survive `lerd lan:unexpose` and silently allow LAN access.
func TestRemoteControlGate_lanOffOverridesCredentials(t *testing.T) {
	// Credentials are set, but lan.exposed is explicitly false.
	setupConfigDirRaw(t, "alice", "s3cret", false)

	next := &nextHandler{}
	gate := withRemoteControlGate(next)

	req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	req.RemoteAddr = "192.168.1.42:54321"
	req.SetBasicAuth("alice", "s3cret")
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (LAN off should override valid creds)", rec.Code)
	}
	if next.called {
		t.Error("LAN client reached the handler despite lan.exposed=false")
	}
}

func TestRemoteControlGate_remoteSetupBypassesAuth(t *testing.T) {
	setupConfigDir(t, "alice", "s3cret") // even with auth set...

	next := &nextHandler{}
	gate := withRemoteControlGate(next)

	req := httptest.NewRequest(http.MethodGet, "/api/remote-setup?code=abc", nil)
	req.RemoteAddr = "192.168.1.42:54321" // ...and a LAN source IP
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if !next.called {
		t.Error("/api/remote-setup did not reach next handler")
	}
}

func TestRemoteControlGate_remoteSetupBypassesEvenWhenDisabled(t *testing.T) {
	setupConfigDir(t, "", "") // remote-control off

	next := &nextHandler{}
	gate := withRemoteControlGate(next)

	req := httptest.NewRequest(http.MethodGet, "/api/remote-setup?code=abc", nil)
	req.RemoteAddr = "192.168.1.42:54321"
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if !next.called {
		t.Error("/api/remote-setup blocked even though it has its own gate")
	}
}

func TestIsLoopbackOnlyPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/api/lerd/stop", true},
		{"/api/lerd/quit", true},
		{"/api/sites/link", true},
		{"/api/browse", true},
		{"/api/sites/myapp.test/terminal", true},
		{"/api/sites/foo.bar.test/terminal", true},
		{"/api/sites", false},
		{"/api/sites/myapp.test", false},
		{"/api/sites/myapp.test/secure", false},
		{"/api/lerd/start", false},
		{"/api/version", false},
		{"/", false},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			if got := isLoopbackOnlyPath(c.path); got != c.want {
				t.Errorf("isLoopbackOnlyPath(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestRemoteControlGate_loopbackOnlyRoutesBlockedFromLAN(t *testing.T) {
	setupConfigDir(t, "alice", "s3cret") // remote-control on with valid creds

	next := &nextHandler{}
	gate := withRemoteControlGate(next)

	cases := []string{
		"/api/lerd/stop",
		"/api/sites/link",
		"/api/sites/myapp.test/terminal",
		"/api/browse",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.RemoteAddr = "192.168.1.42:54321"
			req.SetBasicAuth("alice", "s3cret") // valid creds present
			rec := httptest.NewRecorder()
			gate.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403 (loopback-only path from LAN)", rec.Code)
			}
		})
	}
}

func TestRemoteControlGate_loopbackOnlyRoutesAllowedFromLoopback(t *testing.T) {
	setupConfigDir(t, "", "")

	next := &nextHandler{}
	gate := withRemoteControlGate(next)

	for _, path := range []string{"/api/lerd/stop", "/api/sites/link", "/api/sites/myapp.test/terminal"} {
		t.Run(path, func(t *testing.T) {
			next.called = false
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.RemoteAddr = "127.0.0.1:54321"
			rec := httptest.NewRecorder()
			gate.ServeHTTP(rec, req)
			if !next.called {
				t.Errorf("loopback request to %s blocked", path)
			}
		})
	}
}

func TestRemoteControlGate_optionsBypassesAuth(t *testing.T) {
	setupConfigDir(t, "alice", "s3cret")

	next := &nextHandler{}
	gate := withRemoteControlGate(next)

	req := httptest.NewRequest(http.MethodOptions, "/api/sites", nil)
	req.RemoteAddr = "192.168.1.42:54321" // LAN, no auth header
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if !next.called {
		t.Error("OPTIONS preflight blocked — CORS will fail")
	}
}

// Unix socket connections must be treated as loopback. The lerd.localhost
// nginx vhost reaches lerd-ui over the bind-mounted socket, and the request
// arrives with a non-IP RemoteAddr ("@"). Without the ctxKeyUnixSocket
// fast-path, the gate would 403 it the same as a LAN client and the
// dashboard would be unreachable via lerd.localhost. Regression test for
// the fix that replaced host.containers.internal:7073 with the unix socket.
func TestRemoteControlGate_unixSocketTreatedAsLoopback(t *testing.T) {
	setupConfigDirRaw(t, "", "", false) // LAN exposure off, no creds

	next := &nextHandler{}
	gate := withRemoteControlGate(next)

	req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	req.RemoteAddr = "@" // typical for anonymous unix socket peer
	ctx := context.WithValue(req.Context(), ctxKeyUnixSocket{}, true)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if !next.called {
		t.Error("unix socket request blocked — lerd.localhost vhost will 403")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("unix socket status = %d, want 200", rec.Code)
	}
}

// silence unused-import lint when config is only used transitively.
var _ = config.DataDir
