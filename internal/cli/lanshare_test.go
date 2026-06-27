package cli

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestLANShareRefreshIfRunning_noopWhenNotRunning(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	// No listener registered, refresh should succeed silently.
	if err := LANShareRefreshIfRunning("not-a-site"); err != nil {
		t.Fatalf("refresh on missing site: %v", err)
	}
}

func TestLANShareRefreshIfRunning_restartsListener(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	// Register a fake listener. Refresh should close it. The follow-up
	// re-start will fail (no site in the temp config), and the helper
	// surfaces that error, but the prior server must be gone from the
	// registry either way.
	srv := &http.Server{Handler: http.NotFoundHandler()}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go srv.Serve(ln) //nolint:errcheck

	lanShareMu.Lock()
	lanShareServers["refresh-test"] = srv
	lanShareMu.Unlock()
	t.Cleanup(func() {
		lanShareMu.Lock()
		delete(lanShareServers, "refresh-test")
		lanShareMu.Unlock()
		_ = ln.Close()
	})

	if err := LANShareRefreshIfRunning("refresh-test"); err == nil {
		t.Fatalf("expected refresh to surface an error when site isn't registered")
	}

	lanShareMu.Lock()
	_, stillRegistered := lanShareServers["refresh-test"]
	lanShareMu.Unlock()
	if stillRegistered {
		t.Errorf("expected old server to be removed from registry after refresh")
	}
}

func TestShouldRunLANShareProxy(t *testing.T) {
	cases := []struct {
		name string
		site config.Site
		want bool
	}{
		{"no port", config.Site{Name: "a"}, false},
		{"port + active", config.Site{Name: "a", LANPort: 9100}, true},
		{"port + paused", config.Site{Name: "a", LANPort: 9100, Paused: true}, false},
		{"no port + paused", config.Site{Name: "a", Paused: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldRunLANShareProxy(c.site); got != c.want {
				t.Errorf("shouldRunLANShareProxy(%+v) = %v, want %v", c.site, got, c.want)
			}
		})
	}
}

func TestRewriteLANShareBody_collapsesHTTPSToHTTP(t *testing.T) {
	in := []byte(`<link href="https://laravel.test/build/app.css">
<script src="https://laravel.test/build/app.js"></script>
<a href="http://laravel.test/login">Login</a>`)

	got := string(rewriteLANShareBody(in, "laravel.test", "192.168.1.42:9100"))

	want := `<link href="http://192.168.1.42:9100/build/app.css">
<script src="http://192.168.1.42:9100/build/app.js"></script>
<a href="http://192.168.1.42:9100/login">Login</a>`

	if got != want {
		t.Errorf("rewriteLANShareBody:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRewriteLANShareBody_downgradesAlreadyRewrittenHTTPS(t *testing.T) {
	// Laravel honors X-Forwarded-Host but forces https from APP_URL, so it
	// already emits https://<lanHost>/asset directly. Must be downgraded.
	in := []byte(`<link href="https://192.168.1.42:9100/build/app.css">
<script src="https://192.168.1.42:9100/build/app.js"></script>`)

	got := string(rewriteLANShareBody(in, "laravel.test", "192.168.1.42:9100"))

	want := `<link href="http://192.168.1.42:9100/build/app.css">
<script src="http://192.168.1.42:9100/build/app.js"></script>`

	if got != want {
		t.Errorf("rewriteLANShareBody downgrade:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRewriteLANShareBody_leavesUnrelatedURLsAlone(t *testing.T) {
	in := []byte(`<img src="https://cdn.example.com/logo.png">
<a href="https://other.test/foo">other</a>`)
	got := rewriteLANShareBody(in, "laravel.test", "192.168.1.42:9100")
	if string(got) != string(in) {
		t.Errorf("expected URLs for other domains untouched:\nGOT:\n%s", got)
	}
}

func TestRewriteLANShareBody_rewritesViteLoopback(t *testing.T) {
	// laravel-vite-plugin emits absolute URLs to the Vite dev server, which
	// defaults to localhost/[::1]:5173 and is unreachable from LAN devices.
	// They must be re-pointed through the share proxy's Vite prefix.
	in := []byte(`<script src="http://[::1]:5173/@vite/client"></script>
<script src="http://[::1]:5173/resources/js/app.js"></script>
<link rel="stylesheet" href="http://127.0.0.1:5173/resources/css/app.css">
<script type="module" src="http://localhost:5173/main.js"></script>`)

	got := string(rewriteLANShareBody(in, "laravel.test", "192.168.1.42:9100"))

	want := `<script src="http://192.168.1.42:9100/__lerd_vite__/5173/@vite/client"></script>
<script src="http://192.168.1.42:9100/__lerd_vite__/5173/resources/js/app.js"></script>
<link rel="stylesheet" href="http://192.168.1.42:9100/__lerd_vite__/5173/resources/css/app.css">
<script type="module" src="http://192.168.1.42:9100/__lerd_vite__/5173/main.js"></script>`

	if got != want {
		t.Errorf("rewriteLANShareBody vite rewrite:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRewriteLoopbackViteURLs_rewritesCSSUrlFunction(t *testing.T) {
	// CSS url(...) syntax terminates with ')' not the typical quote/space —
	// the regex must include it so background-image: url(http://[::1]:5173/...)
	// inside Vue scoped styles gets rewritten too.
	in := []byte(`background: url(http://[::1]:5173/images/bg.png);
foo: url('http://127.0.0.1:5173/x.png');
bar: url("http://localhost:5173/y.png");`)

	got := string(rewriteLoopbackViteURLs(in, "192.168.1.42:9100"))

	want := `background: url(http://192.168.1.42:9100/__lerd_vite__/5173/images/bg.png);
foo: url('http://192.168.1.42:9100/__lerd_vite__/5173/x.png');
bar: url("http://192.168.1.42:9100/__lerd_vite__/5173/y.png");`

	if got != want {
		t.Errorf("rewriteLoopbackViteURLs CSS url():\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRewriteLoopbackViteURLs_rewritesJSONEscapedSlashes(t *testing.T) {
	// Inertia.js page payloads embed JSON in the data-page attribute using
	// json_encode, which escapes forward slashes by default. The avatar
	// URLs end up looking like http:\/\/localhost:9000\/starlane\/... —
	// the plain regex skips them because there's no literal `://`. The
	// escaped pass must catch them and emit the same JSON-escape style.
	in := []byte(`<div id="app" data-page='{"props":{"avatar_url":"http:\/\/localhost:9000\/starlane\/avatars\/1\/foo.jpg","other":"http:\/\/127.0.0.1:9000\/bucket"}}'></div>`)

	got := string(rewriteLoopbackViteURLs(in, "192.168.1.42:9100"))

	want := `<div id="app" data-page='{"props":{"avatar_url":"http:\/\/192.168.1.42:9100\/__lerd_vite__\/9000\/starlane\/avatars\/1\/foo.jpg","other":"http:\/\/192.168.1.42:9100\/__lerd_vite__\/9000\/bucket"}}'></div>`

	if got != want {
		t.Errorf("rewriteLoopbackViteURLs JSON-escaped:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRewriteLoopbackViteURLs_skipsShareSelfPort(t *testing.T) {
	// A loopback ref to the share port itself must not loop back through the
	// proxy — leave it alone.
	in := []byte(`<a href="http://localhost:9100/admin">admin</a>`)
	got := rewriteLoopbackViteURLs(in, "192.168.1.42:9100")
	if string(got) != string(in) {
		t.Errorf("expected share-port loopback untouched:\nGOT:\n%s", got)
	}
}

func TestParseVitePrefixPath(t *testing.T) {
	cases := []struct {
		path     string
		wantPort int
		wantRest string
		wantOK   bool
	}{
		{"/__lerd_vite__/5173/@vite/client", 5173, "/@vite/client", true},
		{"/__lerd_vite__/5173/", 5173, "/", true},
		{"/__lerd_vite__/5173", 5173, "/", true},
		{"/__lerd_vite__/5173/path?q=1", 5173, "/path?q=1", true},
		{"/__lerd_vite__/bogus/x", 0, "", false},
		{"/__lerd_vite__/0/x", 0, "", false},
		{"/__lerd_vite__/99999/x", 0, "", false},
		{"/some/other/path", 0, "", false},
		{"/", 0, "", false},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			port, rest, ok := parseVitePrefixPath(c.path)
			if port != c.wantPort || rest != c.wantRest || ok != c.wantOK {
				t.Errorf("parseVitePrefixPath(%q) = (%d, %q, %v), want (%d, %q, %v)",
					c.path, port, rest, ok, c.wantPort, c.wantRest, c.wantOK)
			}
		})
	}
}

func TestLANShareHandler_routesViteRequests(t *testing.T) {
	var gotPath, gotHost string
	vite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHost = r.Host
		fmt.Fprint(w, "vite-response")
	}))
	defer vite.Close()
	vitePort := mustExtractPort(t, vite.URL)

	mainCalls := 0
	main := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mainCalls++
		fmt.Fprint(w, "main-response")
	})

	h := newLANShareHandler(main)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + fmt.Sprintf("/__lerd_vite__/%d/@vite/client", vitePort))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != "vite-response" {
		t.Errorf("body = %q, want vite-response", body)
	}
	if gotPath != "/@vite/client" {
		t.Errorf("vite path = %q, want /@vite/client", gotPath)
	}
	if gotHost != fmt.Sprintf("localhost:%d", vitePort) {
		t.Errorf("vite Host = %q, want localhost:%d", gotHost, vitePort)
	}
	if mainCalls != 0 {
		t.Errorf("main proxy called %d times, want 0", mainCalls)
	}
}

func TestLANShareHandler_nonVitePrefixPathDoesNotPoisonActivePort(t *testing.T) {
	// Loopback URLs in the page body get rewritten through the same
	// /__lerd_vite__/<port>/ prefix regardless of which loopback service
	// they target (Vite, RustFS, mailpit, ...). The handler must NOT mark
	// a port active for transitive-import routing unless the stripped path
	// looks like Vite-served content — otherwise an avatar image at
	// /__lerd_vite__/9000/bucket/key.jpg would poison the active port and
	// the next /node_modules/... request would go to RustFS and 400.
	vite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "vite")
	}))
	defer vite.Close()
	vitePort := mustExtractPort(t, vite.URL)

	rustfs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		fmt.Fprint(w, "jpeg-bytes")
	}))
	defer rustfs.Close()
	rustfsPort := mustExtractPort(t, rustfs.URL)

	mainCalls := 0
	main := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mainCalls++
		fmt.Fprint(w, "main")
	})

	h := newLANShareHandler(main)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 1) Legitimate Vite warmup — path is /@vite/client, matches a Vite
	//    prefix, so activeVitePort must be set.
	resp, err := http.Get(srv.URL + fmt.Sprintf("/__lerd_vite__/%d/@vite/client", vitePort))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if h.getActiveVitePort() != vitePort {
		t.Fatalf("after Vite warmup activeVitePort = %d, want %d", h.getActiveVitePort(), vitePort)
	}

	// 2) Fetch a RustFS-style path through the same prefix mechanism.
	//    Stripped path is /bucket/key.jpg — not Vite-internal. The
	//    request must still proxy correctly to the named port, but it
	//    must NOT change activeVitePort.
	resp, err = http.Get(srv.URL + fmt.Sprintf("/__lerd_vite__/%d/starlane/avatars/1/foo.jpg", rustfsPort))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "jpeg-bytes" {
		t.Errorf("RustFS proxy returned %q, want jpeg-bytes", body)
	}
	if h.getActiveVitePort() != vitePort {
		t.Errorf("activeVitePort poisoned to %d after non-Vite path; want still %d",
			h.getActiveVitePort(), vitePort)
	}

	// 3) Subsequent transitive Vite import (no prefix) must still reach
	//    Vite, not RustFS.
	resp, err = http.Get(srv.URL + "/node_modules/foo.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "vite" {
		t.Errorf("transitive /node_modules/ after RustFS hit returned %q, want vite", body)
	}
	if mainCalls != 0 {
		t.Errorf("main called unexpectedly (%d times)", mainCalls)
	}
}

func TestLANShareHandler_doesNotTrustReferer(t *testing.T) {
	// A Referer header pointing into /__lerd_vite__/<port>/ must NOT route
	// to that port on its own — the share listens on 0.0.0.0 and a LAN
	// device could forge the header to make the proxy dial arbitrary
	// loopback services (SSRF). Without a prior genuine prefix request to
	// set activeVitePort, the request falls through to the main proxy.
	viteCalls := 0
	vite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		viteCalls++
	}))
	defer vite.Close()
	vitePort := mustExtractPort(t, vite.URL)

	mainCalls := 0
	main := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mainCalls++
		fmt.Fprint(w, "main")
	})

	h := newLANShareHandler(main)
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/node_modules/anything.js", nil)
	// Forge a Referer that names the live test Vite — the handler must
	// ignore it.
	req.Header.Set("Referer", srv.URL+fmt.Sprintf("/__lerd_vite__/%d/@vite/client", vitePort))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if viteCalls != 0 {
		t.Errorf("Vite was reached %d times via Referer alone, want 0 (SSRF risk)", viteCalls)
	}
	if mainCalls != 1 {
		t.Errorf("main called %d times, want 1", mainCalls)
	}
}

func TestLANShareHandler_viteInternalPathWithoutReferer_fallsThroughToMain(t *testing.T) {
	mainCalls := 0
	main := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mainCalls++
		fmt.Fprint(w, "main")
	})

	h := newLANShareHandler(main)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/node_modules/something.js")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if mainCalls != 1 {
		t.Errorf("main called %d times, want 1", mainCalls)
	}
}

func TestLANShareHandler_viteInternalPathUsesActivePort(t *testing.T) {
	// After any request has gone through the /__lerd_vite__/<port>/ prefix,
	// subsequent Vite-internal paths without a useful Referer should still
	// route to that same Vite port. Otherwise nested module imports (which
	// drop the prefix from their URL) would 404 on the main proxy.
	var viteHits int
	vite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		viteHits++
		fmt.Fprint(w, "vite")
	}))
	defer vite.Close()
	vitePort := mustExtractPort(t, vite.URL)

	mainCalls := 0
	main := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mainCalls++
		fmt.Fprint(w, "main")
	})

	h := newLANShareHandler(main)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 1) Warm up the handler with a prefix request → sets active port.
	resp, err := http.Get(srv.URL + fmt.Sprintf("/__lerd_vite__/%d/@vite/client", vitePort))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// 2) Subsequent Vite-internal path without Referer should still hit Vite.
	resp, err = http.Get(srv.URL + "/node_modules/.vite/deps/chunk-abc.js?v=1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != "vite" {
		t.Errorf("body = %q, want vite", body)
	}
	if mainCalls != 0 {
		t.Errorf("main called %d times, want 0", mainCalls)
	}
	if viteHits != 2 {
		t.Errorf("vite hits = %d, want 2", viteHits)
	}
}

func TestIsViteInternalPath(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		// Vite-owned prefixes.
		{"/@vite/client", true},
		{"/@id/lodash", true},
		{"/@fs/home/user/project/file.js", true},
		{"/@react-refresh", true},
		{"/node_modules/foo/index.js", true},
		{"/__vite_ping", true},
		{"/__vite_dev/hot", true},
		// Project source roots.
		{"/resources/js/Pages/Home.vue", true},
		{"/src/main.ts", true},
		{"/vendor/tightenco/ziggy/dist/index.js", true},
		// Dev-only file extensions.
		{"/anywhere/file.vue", true},
		{"/x.ts", true},
		{"/x.tsx", true},
		{"/x.jsx", true},
		{"/x.mjs", true},
		{"/x.scss", true},
		{"/x.svelte", true},
		// Vite query hints — even with a generic path.
		// ?v=<hash> is intentionally NOT a Vite signal — it's too generic
		// (every Laravel/Symfony asset uses it for cache busting). Vite's
		// real paths are caught by prefix or extension rules instead.
		{"/something.js?v=abcdef", false},
		{"/something.js?import", true},
		{"/something.css?inline", true},
		// Things nginx serves — must not match.
		{"/", false},
		{"/login", false},
		{"/manifest.json", false},
		{"/favicon.ico", false},
		{"/build/app.css", false}, // Laravel built assets
		{"/images/logo.png", false},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			u, err := url.Parse(c.raw)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", c.raw, err)
			}
			if got := isViteInternalPath(u); got != c.want {
				t.Errorf("isViteInternalPath(%q) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

func TestLANShareHandler_websocketUpgradeRoutesToVite(t *testing.T) {
	// When a WS upgrade arrives at the share root and we have an active
	// Vite port, the handler must dispatch to the Vite proxy rather than
	// nginx. We verify dispatch by recording which sub-handler ran — the
	// proxy itself will fail to actually hijack against httptest's writer,
	// but the dispatch decision is what's under test.
	dispatched := ""
	mainCalls := 0
	main := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mainCalls++
		dispatched = "main"
	})

	h := newLANShareHandler(main)
	// Pre-seed an active vite port so the upgrade has somewhere to go.
	h.setActiveVitePort(54321)
	// Stub the vite proxy entry for that port so we observe dispatch
	// without making a real upstream connection.
	h.viteProxies[54321] = wrapAsReverseProxy(func(w http.ResponseWriter, _ *http.Request) {
		dispatched = "vite"
	})

	req := httptest.NewRequest("GET", "/?token=abc", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if dispatched != "vite" {
		t.Errorf("WS upgrade dispatched to %q, want vite (mainCalls=%d)", dispatched, mainCalls)
	}
}

// wrapAsReverseProxy returns a *httputil.ReverseProxy whose Director and
// transport are overridden so the request is handled by the given fn instead
// of dialing upstream. Used to observe dispatch without a real backend.
func wrapAsReverseProxy(fn http.HandlerFunc) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Director: func(*http.Request) {},
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			fn(rec, r)
			return rec.Result(), nil
		}),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestIsWebSocketUpgrade(t *testing.T) {
	cases := []struct {
		name string
		hdr  http.Header
		want bool
	}{
		{"plain upgrade", http.Header{"Upgrade": []string{"websocket"}, "Connection": []string{"Upgrade"}}, true},
		{"connection has extra tokens", http.Header{"Upgrade": []string{"websocket"}, "Connection": []string{"keep-alive, Upgrade"}}, true},
		{"mixed case", http.Header{"Upgrade": []string{"WebSocket"}, "Connection": []string{"UPGRADE"}}, true},
		{"upgrade missing", http.Header{"Connection": []string{"Upgrade"}}, false},
		{"upgrade is not websocket", http.Header{"Upgrade": []string{"h2c"}, "Connection": []string{"Upgrade"}}, false},
		{"plain GET", http.Header{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &http.Request{Header: c.hdr}
			if got := isWebSocketUpgrade(r); got != c.want {
				t.Errorf("isWebSocketUpgrade(%v) = %v, want %v", c.hdr, got, c.want)
			}
		})
	}
}

func TestLANShareHandler_routesMainRequests(t *testing.T) {
	mainCalls := 0
	main := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mainCalls++
		fmt.Fprint(w, "main-response")
	})

	h := newLANShareHandler(main)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(body) != "main-response" {
		t.Errorf("body = %q, want main-response", body)
	}
	if mainCalls != 1 {
		t.Errorf("main proxy called %d times, want 1", mainCalls)
	}
}

func mustExtractPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	var p int
	if _, err := fmt.Sscanf(u.Port(), "%d", &p); err != nil {
		t.Fatalf("parse port from %q: %v", u.Port(), err)
	}
	return p
}

func TestLANShareProxy_rewritesHTTPSLocationRedirects(t *testing.T) {
	// Upstream stands in for nginx/the app. It builds a redirect Location from
	// the path: /to-domain uses the origin domain, /to-lanhost uses the
	// forwarded LAN host with a forced https scheme (the case Laravel hits when
	// it honors X-Forwarded-Host but forces https from APP_URL).
	const domain = "laravel.test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var loc string
		switch r.URL.Path {
		case "/to-domain":
			loc = "https://" + domain + "/dashboard"
		case "/to-lanhost":
			loc = "https://" + r.Header.Get("X-Forwarded-Host") + "/dashboard"
		}
		w.Header().Set("Location", loc)
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()
	upstreamPort := mustExtractPort(t, upstream.URL)

	// Reserve a free port for the proxy, then hand it to startLANShareProxy.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	proxyPort := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	srv, err := startLANShareProxy(domain, proxyPort, upstreamPort, 0, false)
	if err != nil {
		t.Fatalf("startLANShareProxy: %v", err)
	}
	defer srv.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	lanHost := fmt.Sprintf("127.0.0.1:%d", proxyPort)
	wantLoc := "http://" + lanHost + "/dashboard"

	for _, path := range []string{"/to-domain", "/to-lanhost"} {
		t.Run(path, func(t *testing.T) {
			resp, err := client.Get("http://" + lanHost + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			resp.Body.Close()
			if got := resp.Header.Get("Location"); got != wantLoc {
				t.Errorf("Location = %q, want %q", got, wantLoc)
			}
		})
	}
}

func TestRewriteLANShareBody_fixesLANIPWithWrongPort(t *testing.T) {
	// Laravel/Symfony can emit URLs with the LAN IP but SERVER_PORT (443 from
	// the nginx HTTPS vhost) when X-Forwarded-Port isn't honored — those must
	// be normalized to the actual LAN share port.
	in := []byte(`<link rel="manifest" href="http://192.168.1.42:443/manifest.json">
<script src="https://192.168.1.42:443/build/app.js"></script>
<img src="http://192.168.1.42/logo.png">`)

	got := string(rewriteLANShareBody(in, "laravel.test", "192.168.1.42:9100"))

	want := `<link rel="manifest" href="http://192.168.1.42:9100/manifest.json">
<script src="http://192.168.1.42:9100/build/app.js"></script>
<img src="http://192.168.1.42:9100/logo.png">`

	if got != want {
		t.Errorf("rewriteLANShareBody port-fix:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}
