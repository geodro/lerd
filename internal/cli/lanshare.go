package cli

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/geodro/lerd/internal/config"
)

var (
	lanShareMu      sync.Mutex
	lanShareServers = map[string]*http.Server{} // siteName → running proxy
)

// LANShareEnsurePort assigns a stable port to the site if not already set and
// saves it to sites.yaml. It does NOT start the proxy — that is the daemon's job.
// CLI commands use this to persist the port, then notify the daemon via its API.
func LANShareEnsurePort(siteName string) (int, error) {
	site, err := config.FindSite(siteName)
	if err != nil {
		return 0, err
	}
	if site.LANPort != 0 {
		return site.LANPort, nil
	}
	port := assignLANSharePort(siteName)
	site.LANPort = port
	if err := config.AddSite(*site); err != nil {
		return 0, fmt.Errorf("saving LAN port: %w", err)
	}
	return port, nil
}

// LANShareStart starts the in-process reverse proxy for the site. It is
// intended to be called from the daemon (UI server) only — the proxy goroutine
// lives in the daemon process. CLI commands should notify the daemon via its
// HTTP API instead of calling this directly.
func LANShareStart(siteName string) (int, error) {
	site, err := config.FindSite(siteName)
	if err != nil {
		return 0, err
	}

	port := site.LANPort
	if port == 0 {
		port = assignLANSharePort(siteName)
		site.LANPort = port
		if err := config.AddSite(*site); err != nil {
			return 0, fmt.Errorf("saving LAN port: %w", err)
		}
	}

	lanShareMu.Lock()
	if _, running := lanShareServers[siteName]; running {
		lanShareMu.Unlock()
		return port, nil
	}
	lanShareMu.Unlock()

	cfg, _ := config.LoadGlobal()
	httpPort := cfg.Nginx.HTTPPort
	if httpPort == 0 {
		httpPort = 80
	}
	httpsPort := cfg.Nginx.HTTPSPort
	if httpsPort == 0 {
		httpsPort = 443
	}

	srv, err := startLANShareProxy(site.PrimaryDomain(), port, httpPort, httpsPort, site.Secured)
	if err != nil {
		return 0, err
	}

	lanShareMu.Lock()
	lanShareServers[siteName] = srv
	lanShareMu.Unlock()

	return port, nil
}

// LANShareStop stops the LAN share proxy for the site and clears its port
// from the site registry.
func LANShareStop(siteName string) error {
	lanShareMu.Lock()
	srv, running := lanShareServers[siteName]
	if running {
		delete(lanShareServers, siteName)
	}
	lanShareMu.Unlock()

	if running {
		srv.Close()
	}

	site, err := config.FindSite(siteName)
	if err != nil {
		return err
	}
	site.LANPort = 0
	return config.AddSite(*site)
}

// LANShareRunning reports whether a share proxy is active for the site.
func LANShareRunning(siteName string) bool {
	lanShareMu.Lock()
	defer lanShareMu.Unlock()
	_, ok := lanShareServers[siteName]
	return ok
}

// RestoreLANShareProxies restarts share proxies for every site that has a
// LANPort stored in the registry. Called once when the UI server starts.
func RestoreLANShareProxies() {
	reg, err := config.LoadSites()
	if err != nil {
		return
	}
	cfg, _ := config.LoadGlobal()
	httpPort := 80
	httpsPort := 443
	if cfg != nil {
		if cfg.Nginx.HTTPPort != 0 {
			httpPort = cfg.Nginx.HTTPPort
		}
		if cfg.Nginx.HTTPSPort != 0 {
			httpsPort = cfg.Nginx.HTTPSPort
		}
	}
	for _, s := range reg.Sites {
		if s.LANPort == 0 {
			continue
		}
		srv, err := startLANShareProxy(s.PrimaryDomain(), s.LANPort, httpPort, httpsPort, s.Secured)
		if err != nil {
			continue
		}
		lanShareMu.Lock()
		lanShareServers[s.Name] = srv
		lanShareMu.Unlock()
	}
}

// assignLANSharePort finds the lowest unused port >= 9100 across all sites.
func assignLANSharePort(excludeSiteName string) int {
	const base = 9100
	used := map[int]bool{}
	reg, err := config.LoadSites()
	if err != nil {
		return base
	}
	for _, s := range reg.Sites {
		if s.Name == excludeSiteName || s.LANPort == 0 {
			continue
		}
		used[s.LANPort] = true
	}
	port := base
	for used[port] {
		port++
	}
	return port
}

// startLANShareProxy starts an HTTP reverse proxy listening on 0.0.0.0:<port>.
// It rewrites the Host header to domain so nginx routes to the right vhost,
// sets X-Forwarded-Host to the incoming address, and rewrites response bodies
// and Location headers to replace domain URLs with the LAN address.
func startLANShareProxy(domain string, port, httpPort, httpsPort int, secured bool) (*http.Server, error) {
	var target *url.URL
	if secured {
		target = &url.URL{Scheme: "https", Host: fmt.Sprintf("localhost:%d", httpsPort)}
	} else {
		target = &url.URL{Scheme: "http", Host: fmt.Sprintf("localhost:%d", httpPort)}
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	if secured {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, ServerName: domain} //nolint:gosec
		proxy.Transport = t
	}

	const scheme = "http"

	orig := proxy.Director
	proxy.Director = func(req *http.Request) {
		lanHost := req.Host // e.g. "192.168.1.5:9100"
		orig(req)
		req.Header.Set("X-Forwarded-Host", lanHost)
		req.Header.Set("X-Forwarded-Proto", scheme)
		req.Host = domain
		// Tell upstream not to compress so we can rewrite bodies without
		// having to decompress/recompress every response.
		req.Header.Set("Accept-Encoding", "identity")
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		lanHost := resp.Request.Header.Get("X-Forwarded-Host")
		if lanHost == "" {
			return nil
		}

		// Rewrite Location headers: replace both http:// and https:// variants
		// of the origin domain with the plain-HTTP LAN address.
		if loc := resp.Header.Get("Location"); loc != "" {
			loc = strings.ReplaceAll(loc, "https://"+domain, scheme+"://"+lanHost)
			loc = strings.ReplaceAll(loc, "http://"+domain, scheme+"://"+lanHost)
			resp.Header.Set("Location", loc)
		}

		ct := resp.Header.Get("Content-Type")
		enc := resp.Header.Get("Content-Encoding")

		if !isTextContent(ct) {
			return nil
		}

		// Read the body, decompressing gzip if needed.
		var body []byte
		var err error
		if enc == "gzip" {
			gr, gErr := gzip.NewReader(resp.Body)
			if gErr != nil {
				return nil // leave untouched if we can't decompress
			}
			body, err = io.ReadAll(gr)
			gr.Close()
			resp.Body.Close()
			if err != nil {
				return err
			}
			resp.Header.Del("Content-Encoding")
		} else if enc == "" || enc == "identity" {
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
		} else {
			return nil // unknown encoding, leave untouched
		}

		// Replace https://domain and http://domain with the LAN address.
		body = bytes.ReplaceAll(body, []byte("https://"+domain), []byte(scheme+"://"+lanHost))
		body = bytes.ReplaceAll(body, []byte("http://"+domain), []byte(scheme+"://"+lanHost))

		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
		return nil
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return nil, fmt.Errorf("binding port %d: %w", port, err)
	}

	srv := &http.Server{Handler: proxy}
	go srv.Serve(ln) //nolint:errcheck
	return srv, nil
}

// PrintLANShareQR prints a compact QR code for the given URL to stdout using
// half-block Unicode characters (two rows per terminal line).
func PrintLANShareQR(rawURL string) {
	qr, err := qrcode.New(rawURL, qrcode.Medium)
	if err != nil {
		return
	}
	qr.DisableBorder = false
	bitmap := qr.Bitmap() // [][]bool, true = dark module

	// Render two rows of modules per terminal line using half-block chars.
	// Quiet zone is already included in Bitmap().
	for y := 0; y < len(bitmap); y += 2 {
		row1 := bitmap[y]
		var row2 []bool
		if y+1 < len(bitmap) {
			row2 = bitmap[y+1]
		} else {
			row2 = make([]bool, len(row1))
		}
		for x := range row1 {
			top := row1[x]
			bot := row2[x]
			switch {
			case top && bot:
				fmt.Fprint(os.Stdout, "█")
			case top:
				fmt.Fprint(os.Stdout, "▀")
			case bot:
				fmt.Fprint(os.Stdout, "▄")
			default:
				fmt.Fprint(os.Stdout, " ")
			}
		}
		fmt.Fprintln(os.Stdout)
	}
}

// LANShareURL returns the URL a LAN device would use to reach the site on the
// given port, or an empty string if the port is 0 or the LAN IP cannot be detected.
func LANShareURL(lanPort int) string {
	if lanPort == 0 {
		return ""
	}
	ip, err := detectPrimaryLANIP()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", ip, lanPort)
}
