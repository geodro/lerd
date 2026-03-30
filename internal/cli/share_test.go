package cli

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── isTextContent ─────────────────────────────────────────────────────────────

func TestIsTextContent(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"text/html; charset=utf-8", true},
		{"text/html", true},
		{"text/css", true},
		{"application/javascript", true},
		{"text/javascript", true},
		{"application/json", true},
		{"image/png", false},
		{"image/jpeg", false},
		{"application/octet-stream", false},
		{"application/pdf", false},
		{"", false},
	}
	for _, c := range cases {
		got := isTextContent(c.ct)
		if got != c.want {
			t.Errorf("isTextContent(%q) = %v, want %v", c.ct, got, c.want)
		}
	}
}

// ── pickShareTool ─────────────────────────────────────────────────────────────

// fakeBin creates a fake executable named binName in a temp dir and returns
// a PATH that contains only that directory (plus an optional extra dir).
func fakeBin(t *testing.T, binNames ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range binNames {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestPickShareTool_mutualExclusion(t *testing.T) {
	pairs := [][5]bool{
		{true, true, false, false, false}, // ngrok + cloudflare
		{true, false, true, false, false}, // ngrok + expose
		{false, true, true, false, false}, // cloudflare + expose
		{false, false, true, true, false}, // expose + serveo
		{true, false, false, false, true}, // ngrok + localhost-run
	}
	for _, p := range pairs {
		_, err := pickShareTool(p[0], p[1], p[2], p[3], p[4])
		if err == nil {
			t.Errorf("pickShareTool%v: expected mutual-exclusion error, got nil", p)
			continue
		}
		if !strings.Contains(err.Error(), "only one of") {
			t.Errorf("pickShareTool%v: error %q does not contain 'only one of'", p, err)
		}
	}
}

func TestPickShareTool_explicitNgrok_present(t *testing.T) {
	t.Setenv("PATH", fakeBin(t, "ngrok"))
	tool, err := pickShareTool(true, false, false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.mode != shareModeNgrok {
		t.Errorf("mode = %v, want shareModeNgrok", tool.mode)
	}
}

func TestPickShareTool_explicitNgrok_absent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := pickShareTool(true, false, false, false, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "ngrok not found") {
		t.Errorf("error %q does not mention ngrok", err)
	}
}

func TestPickShareTool_explicitCloudflare_present(t *testing.T) {
	t.Setenv("PATH", fakeBin(t, "cloudflared"))
	tool, err := pickShareTool(false, true, false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.mode != shareModeCloudflare {
		t.Errorf("mode = %v, want shareModeCloudflare", tool.mode)
	}
}

func TestPickShareTool_explicitCloudflare_absent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := pickShareTool(false, true, false, false, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cloudflared not found") {
		t.Errorf("error %q does not mention cloudflared", err)
	}
}

func TestPickShareTool_explicitExpose_present(t *testing.T) {
	t.Setenv("PATH", fakeBin(t, "expose"))
	tool, err := pickShareTool(false, false, true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.mode != shareModeExpose {
		t.Errorf("mode = %v, want shareModeExpose", tool.mode)
	}
}

func TestPickShareTool_explicitExpose_absent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := pickShareTool(false, false, true, false, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "expose not found") {
		t.Errorf("error %q does not mention expose", err)
	}
}

func TestPickShareTool_explicitServeo(t *testing.T) {
	tool, err := pickShareTool(false, false, false, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.mode != shareModeSSH {
		t.Errorf("mode = %v, want shareModeSSH", tool.mode)
	}
	if tool.sshHost != "serveo.net" {
		t.Errorf("sshHost = %q, want serveo.net", tool.sshHost)
	}
}

func TestPickShareTool_explicitLocalhostRun(t *testing.T) {
	tool, err := pickShareTool(false, false, false, false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.mode != shareModeSSH {
		t.Errorf("mode = %v, want shareModeSSH", tool.mode)
	}
	if tool.sshHost != "localhost.run" {
		t.Errorf("sshHost = %q, want localhost.run", tool.sshHost)
	}
}

func TestPickShareTool_autoDetect_ngrokFirst(t *testing.T) {
	t.Setenv("PATH", fakeBin(t, "ngrok", "cloudflared", "expose", "ssh"))
	tool, err := pickShareTool(false, false, false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.mode != shareModeNgrok {
		t.Errorf("mode = %v, want shareModeNgrok (ngrok takes priority)", tool.mode)
	}
}

func TestPickShareTool_autoDetect_cloudflareBeforeExpose(t *testing.T) {
	t.Setenv("PATH", fakeBin(t, "cloudflared", "expose", "ssh"))
	tool, err := pickShareTool(false, false, false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.mode != shareModeCloudflare {
		t.Errorf("mode = %v, want shareModeCloudflare", tool.mode)
	}
}

func TestPickShareTool_autoDetect_expose(t *testing.T) {
	t.Setenv("PATH", fakeBin(t, "expose", "ssh"))
	tool, err := pickShareTool(false, false, false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.mode != shareModeExpose {
		t.Errorf("mode = %v, want shareModeExpose", tool.mode)
	}
}

func TestPickShareTool_autoDetect_sshFallback(t *testing.T) {
	t.Setenv("PATH", fakeBin(t, "ssh"))
	tool, err := pickShareTool(false, false, false, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool.mode != shareModeSSH {
		t.Errorf("mode = %v, want shareModeSSH", tool.mode)
	}
	if tool.sshHost != "localhost.run" {
		t.Errorf("sshHost = %q, want localhost.run", tool.sshHost)
	}
}

func TestPickShareTool_autoDetect_noTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := pickShareTool(false, false, false, false, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no tunnel tool found") {
		t.Errorf("error %q does not mention 'no tunnel tool found'", err)
	}
}

// ── startHostProxy ────────────────────────────────────────────────────────────

// proxyPort parses the port from a URL string like "http://127.0.0.1:PORT".
func proxyPort(t *testing.T, rawURL string) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(rawURL, "http://"), "https://"))
	if err != nil {
		t.Fatalf("parsing port from %q: %v", rawURL, err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	return port
}

// doProxy sends a GET to the proxy with the given Host header and returns the response.
func doProxy(t *testing.T, port int, host string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	return resp
}

func TestStartHostProxy_rewritesHostHeader(t *testing.T) {
	var gotHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	port, stop, err := startHostProxy("mysite.test", proxyPort(t, backend.URL), 0, false)
	if err != nil {
		t.Fatalf("startHostProxy: %v", err)
	}
	defer stop()

	resp := doProxy(t, port, "abc.trycloudflare.com")
	resp.Body.Close()

	if gotHost != "mysite.test" {
		t.Errorf("backend Host = %q, want %q", gotHost, "mysite.test")
	}
}

func TestStartHostProxy_setsForwardedHeaders(t *testing.T) {
	var gotXFH, gotXFP string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXFH = r.Header.Get("X-Forwarded-Host")
		gotXFP = r.Header.Get("X-Forwarded-Proto")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	port, stop, err := startHostProxy("mysite.test", proxyPort(t, backend.URL), 0, false)
	if err != nil {
		t.Fatalf("startHostProxy: %v", err)
	}
	defer stop()

	resp := doProxy(t, port, "abc.trycloudflare.com")
	resp.Body.Close()

	if gotXFH != "abc.trycloudflare.com" {
		t.Errorf("X-Forwarded-Host = %q, want %q", gotXFH, "abc.trycloudflare.com")
	}
	if gotXFP != "https" {
		t.Errorf("X-Forwarded-Proto = %q, want https", gotXFP)
	}
}

func TestStartHostProxy_rewritesLocationHeader(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://mysite.test/dashboard")
		w.WriteHeader(http.StatusFound)
	}))
	defer backend.Close()

	port, stop, err := startHostProxy("mysite.test", proxyPort(t, backend.URL), 0, false)
	if err != nil {
		t.Fatalf("startHostProxy: %v", err)
	}
	defer stop()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
	req.Host = "abc.trycloudflare.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "abc.trycloudflare.com") {
		t.Errorf("Location = %q, want it to contain tunnel host", loc)
	}
	if strings.Contains(loc, "mysite.test") {
		t.Errorf("Location = %q still contains local domain", loc)
	}
}

func TestStartHostProxy_rewritesLocationHeader_httpToHttps(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://mysite.test/login")
		w.WriteHeader(http.StatusFound)
	}))
	defer backend.Close()

	port, stop, err := startHostProxy("mysite.test", proxyPort(t, backend.URL), 0, false)
	if err != nil {
		t.Fatalf("startHostProxy: %v", err)
	}
	defer stop()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
	req.Host = "tunnel.example.com"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "https://") {
		t.Errorf("Location = %q, want https:// prefix after rewrite", loc)
	}
}

func TestStartHostProxy_rewritesHTMLBody(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<a href="http://mysite.test/page">link</a>`)
	}))
	defer backend.Close()

	port, stop, err := startHostProxy("mysite.test", proxyPort(t, backend.URL), 0, false)
	if err != nil {
		t.Fatalf("startHostProxy: %v", err)
	}
	defer stop()

	resp := doProxy(t, port, "abc.trycloudflare.com")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if strings.Contains(string(body), "mysite.test") {
		t.Errorf("body still contains local domain: %s", body)
	}
	if !strings.Contains(string(body), "abc.trycloudflare.com") {
		t.Errorf("body does not contain tunnel host: %s", body)
	}
}

func TestStartHostProxy_doesNotRewriteBinaryBody(t *testing.T) {
	original := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(original)
	}))
	defer backend.Close()

	port, stop, err := startHostProxy("mysite.test", proxyPort(t, backend.URL), 0, false)
	if err != nil {
		t.Fatalf("startHostProxy: %v", err)
	}
	defer stop()

	resp := doProxy(t, port, "abc.trycloudflare.com")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != string(original) {
		t.Errorf("binary body was modified")
	}
}

func TestStartHostProxy_listensOnLoopback(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	port, stop, err := startHostProxy("mysite.test", proxyPort(t, backend.URL), 0, false)
	if err != nil {
		t.Fatalf("startHostProxy: %v", err)
	}
	defer stop()

	// Must be reachable on 127.0.0.1 (not just ::1)
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatalf("proxy not reachable on 127.0.0.1: %v", err)
	}
	resp.Body.Close()
}

func TestStartHostProxy_stopClosesListener(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	port, stop, err := startHostProxy("mysite.test", proxyPort(t, backend.URL), 0, false)
	if err != nil {
		t.Fatalf("startHostProxy: %v", err)
	}
	stop()

	_, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err == nil {
		t.Error("expected connection error after stop, got nil")
	}
}
