package cli

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"
)

// wsEchoBackend performs a raw WebSocket-style 101 upgrade and echoes one line,
// standing in for nginx->reverb / nginx->noVNC.
func wsEchoBackend(tag string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			conn, brw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer conn.Close()
			io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			brw.Flush()
			line, _ := brw.ReadString('\n')
			io.WriteString(conn, tag+":"+line)
			return
		}
		fmt.Fprint(w, "plain")
	}))
}

// dialWS sends a raw WS handshake to host on path and returns the status line
// plus the post-handshake echo (or an error on hang/timeout).
func dialWS(host, path string) (status, echo string, err error) {
	conn, err := net.DialTimeout("tcp", host, 3*time.Second)
	if err != nil {
		return "", "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	io.WriteString(conn, "GET "+path+" HTTP/1.1\r\nHost: "+host+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: x\r\nSec-WebSocket-Version: 13\r\n\r\n")
	br := bufio.NewReader(conn)
	status, err = br.ReadString('\n')
	if err != nil {
		return "", "", err
	}
	for {
		l, e := br.ReadString('\n')
		if e != nil || l == "\r\n" || l == "" {
			break
		}
	}
	io.WriteString(conn, "hello\n")
	echo, _ = br.ReadString('\n')
	return status, echo, nil
}

// realMainProxy mirrors the production main proxy built in startLANShareProxy:
// a single-host reverse proxy to the (nginx) upstream with the same Director
// header rewrites.
func realMainProxy(t *testing.T, upstream string) *httputil.ReverseProxy {
	t.Helper()
	tu, _ := url.Parse(upstream)
	p := httputil.NewSingleHostReverseProxy(tu)
	orig := p.Director
	p.Director = func(req *http.Request) {
		lanHost := req.Host
		orig(req)
		req.Header.Set("X-Forwarded-Host", lanHost)
		req.Header.Set("X-Forwarded-Proto", "http")
		req.Host = tu.Host
		req.Header.Set("Accept-Encoding", "identity")
	}
	return p
}

// No Vite active: an app WS through the real lanShareHandler must upgrade
// end-to-end via the main proxy.
func TestLANShareE2E_appWebsocket_noVite(t *testing.T) {
	be := wsEchoBackend("nginx")
	defer be.Close()

	h := newLANShareHandler(realMainProxy(t, be.URL))
	srv := httptest.NewServer(h)
	defer srv.Close()
	host := mustHost(t, srv.URL)

	status, echo, err := dialWS(host, "/app/local-key?protocol=7")
	if err != nil {
		t.Fatalf("WS dial failed (HANG): %v", err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("status = %q, want 101", status)
	}
	if !strings.HasPrefix(echo, "nginx:") {
		t.Fatalf("echo = %q, want nginx: prefix", echo)
	}
}

// Vite active: an app WS (real path) must still reach the main proxy, not get
// hijacked to the Vite dev server. This is the #656 regression.
func TestLANShareE2E_appWebsocket_viteActive(t *testing.T) {
	be := wsEchoBackend("nginx")
	defer be.Close()
	// A Vite stand-in that does NOT speak the app's WS protocol — if the app
	// socket were routed here it would hang on the echo read.
	vite := wsEchoBackend("vite")
	defer vite.Close()
	vitePort := mustExtractPort(t, vite.URL)

	h := newLANShareHandler(realMainProxy(t, be.URL))
	h.setActiveVitePort(vitePort)
	srv := httptest.NewServer(h)
	defer srv.Close()
	host := mustHost(t, srv.URL)

	for _, path := range []string{"/app/local-key?protocol=7", "/websockify"} {
		status, echo, err := dialWS(host, path)
		if err != nil {
			t.Fatalf("[%s] WS dial failed (HANG): %v", path, err)
		}
		if !strings.Contains(status, "101") {
			t.Fatalf("[%s] status = %q, want 101", path, status)
		}
		if !strings.HasPrefix(echo, "nginx:") {
			t.Fatalf("[%s] echo = %q, want nginx: (app WS hijacked to Vite)", path, echo)
		}
	}
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}
