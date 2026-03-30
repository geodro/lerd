package cli

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
)

// NewShareCmd returns the share command.
func NewShareCmd() *cobra.Command {
	var useNgrok bool
	var useCloudflare bool
	var useExpose bool
	var useServeo bool
	var useLocalhostRun bool

	cmd := &cobra.Command{
		Use:   "share [site]",
		Short: "Expose the current site publicly via a tunnel",
		Long: `Starts a public tunnel to the current site.

Auto-detection order: ngrok → Cloudflare Tunnel → Expose → localhost.run (SSH, no signup).

Supported tools:
  ngrok          https://ngrok.com/download
  cloudflared    https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/
  expose         https://expose.dev
  localhost.run  free SSH tunnel, no account needed (--localhost-run)
  serveo.net     free SSH tunnel, no account needed (--serveo)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runShare(args, useNgrok, useCloudflare, useExpose, useServeo, useLocalhostRun)
		},
	}

	cmd.Flags().BoolVar(&useNgrok, "ngrok", false, "Use ngrok")
	cmd.Flags().BoolVar(&useCloudflare, "cloudflare", false, "Use Cloudflare Tunnel (cloudflared)")
	cmd.Flags().BoolVar(&useExpose, "expose", false, "Use Expose")
	cmd.Flags().BoolVar(&useServeo, "serveo", false, "Use serveo.net (SSH, no signup)")
	cmd.Flags().BoolVar(&useLocalhostRun, "localhost-run", false, "Use localhost.run (SSH, no signup)")
	return cmd
}

type shareMode int

const (
	shareModeNgrok      shareMode = iota
	shareModeExpose     shareMode = iota
	shareModeSSH        shareMode = iota
	shareModeCloudflare shareMode = iota
)

type shareTool struct {
	mode    shareMode
	sshHost string // only for shareModeSSH
}

func runShare(args []string, useNgrok, useCloudflare, useExpose, useServeo, useLocalhostRun bool) error {
	site, err := resolveShareSite(args)
	if err != nil {
		return err
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	port := cfg.Nginx.HTTPPort
	if port == 0 {
		port = 80
	}

	tool, err := pickShareTool(useNgrok, useCloudflare, useExpose, useServeo, useLocalhostRun)
	if err != nil {
		return err
	}

	fmt.Printf("Sharing %s...\n", site.Domain)
	fmt.Println("Press Ctrl+C to stop.")
	fmt.Println()

	var cmd *exec.Cmd
	switch tool.mode {
	case shareModeNgrok:
		cmd = exec.Command("ngrok", "http", fmt.Sprintf("%d", port),
			"--host-header="+site.Domain)
	case shareModeExpose:
		shareURL := fmt.Sprintf("http://%s", site.Domain)
		if port != 80 {
			shareURL = fmt.Sprintf("http://%s:%d", site.Domain, port)
		}
		cmd = exec.Command("expose", "share", shareURL)
	case shareModeCloudflare:
		httpsPort := cfg.Nginx.HTTPSPort
		if httpsPort == 0 {
			httpsPort = 443
		}
		proxyPort, stop, err := startHostProxy(site.Domain, port, httpsPort, site.Secured)
		if err != nil {
			return fmt.Errorf("starting local proxy: %w", err)
		}
		defer stop()
		cmd = exec.Command("cloudflared", "tunnel",
			"--url", fmt.Sprintf("http://127.0.0.1:%d", proxyPort))
	case shareModeSSH:
		httpsPort := cfg.Nginx.HTTPSPort
		if httpsPort == 0 {
			httpsPort = 443
		}
		// Start a local reverse proxy that rewrites Host so nginx routes correctly.
		proxyPort, stop, err := startHostProxy(site.Domain, port, httpsPort, site.Secured)
		if err != nil {
			return fmt.Errorf("starting local proxy: %w", err)
		}
		defer stop()

		fmt.Printf("Local proxy started on port %d (Host: %s → nginx:%d)\n\n", proxyPort, site.Domain, port)

		cmd = exec.Command("ssh",
			"-o", "StrictHostKeyChecking=no",
			"-o", "ServerAliveInterval=30",
			"-R", fmt.Sprintf("80:localhost:%d", proxyPort),
			"nokey@"+tool.sshHost,
		)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// resolveShareSite finds the site for the given name arg (or CWD if no arg).
func resolveShareSite(args []string) (*config.Site, error) {
	if len(args) == 1 {
		site, err := config.FindSite(args[0])
		if err != nil {
			return nil, fmt.Errorf("site %q not found — run 'lerd sites' to list registered sites", args[0])
		}
		return site, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	site, err := config.FindSiteByPath(cwd)
	if err == nil {
		return site, nil
	}

	// Fall back: treat the directory name as a site name.
	name, _ := siteNameAndDomain(filepath.Base(cwd), "test")
	site, err = config.FindSite(name)
	if err != nil {
		return nil, fmt.Errorf("no registered site found for this directory — run 'lerd link' first")
	}
	return site, nil
}

func pickShareTool(useNgrok, useCloudflare, useExpose, useServeo, useLocalhostRun bool) (*shareTool, error) {
	count := 0
	for _, f := range []bool{useNgrok, useCloudflare, useExpose, useServeo, useLocalhostRun} {
		if f {
			count++
		}
	}
	if count > 1 {
		return nil, fmt.Errorf("only one of --ngrok, --cloudflare, --expose, --serveo, --localhost-run may be specified")
	}

	if useNgrok {
		if _, err := exec.LookPath("ngrok"); err != nil {
			return nil, fmt.Errorf("ngrok not found in PATH — install it from https://ngrok.com/download")
		}
		return &shareTool{mode: shareModeNgrok}, nil
	}
	if useCloudflare {
		if _, err := exec.LookPath("cloudflared"); err != nil {
			return nil, fmt.Errorf("cloudflared not found in PATH — install it from https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/")
		}
		return &shareTool{mode: shareModeCloudflare}, nil
	}
	if useExpose {
		if _, err := exec.LookPath("expose"); err != nil {
			return nil, fmt.Errorf("expose not found in PATH — install it from https://expose.dev")
		}
		return &shareTool{mode: shareModeExpose}, nil
	}
	if useServeo {
		return &shareTool{mode: shareModeSSH, sshHost: "serveo.net"}, nil
	}
	if useLocalhostRun {
		return &shareTool{mode: shareModeSSH, sshHost: "localhost.run"}, nil
	}

	// Auto-detect.
	if _, err := exec.LookPath("ngrok"); err == nil {
		return &shareTool{mode: shareModeNgrok}, nil
	}
	if _, err := exec.LookPath("cloudflared"); err == nil {
		return &shareTool{mode: shareModeCloudflare}, nil
	}
	if _, err := exec.LookPath("expose"); err == nil {
		return &shareTool{mode: shareModeExpose}, nil
	}
	if _, err := exec.LookPath("ssh"); err == nil {
		fmt.Println("ngrok/cloudflared/Expose not found — using localhost.run (SSH, no signup required)")
		return &shareTool{mode: shareModeSSH, sshHost: "localhost.run"}, nil
	}

	return nil, fmt.Errorf("no tunnel tool found — install ngrok (https://ngrok.com/download), cloudflared (https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/), or Expose (https://expose.dev), or ensure ssh is in PATH")
}

// startHostProxy starts a local HTTP reverse proxy on a random loopback port.
// It rewrites the Host header to domain before forwarding to nginx,
// so nginx can route the request to the correct vhost.
// For secured (TLS) sites it connects to the HTTPS port to avoid the HTTP→HTTPS
// redirect loop; for plain HTTP sites it connects to httpPort.
// It also rewrites Location response headers, replacing the local domain with the
// tunnel host so browser redirects don't escape to the local .test URL.
// Returns the chosen port and a stop function.
func startHostProxy(domain string, httpPort, httpsPort int, secured bool) (int, func(), error) {
	var target *url.URL
	if secured {
		target = &url.URL{Scheme: "https", Host: fmt.Sprintf("localhost:%d", httpsPort)}
	} else {
		target = &url.URL{Scheme: "http", Host: fmt.Sprintf("localhost:%d", httpPort)}
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	// For secured sites the upstream uses a local mkcert cert — clone the default
	// transport (preserving connection pooling/timeouts) and add TLS skip-verify.
	if secured {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true, ServerName: domain} //nolint:gosec
		proxy.Transport = t
	}

	orig := proxy.Director
	proxy.Director = func(req *http.Request) {
		// Save the incoming tunnel host before overwriting.
		tunnelHost := req.Host
		orig(req)
		// Tell Laravel (and any other framework) the real public host/scheme.
		req.Header.Set("X-Forwarded-Host", tunnelHost)
		req.Header.Set("X-Forwarded-Proto", "https")
		// Rewrite Host so nginx routes to the right vhost.
		req.Host = domain
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		tunnelHost := resp.Request.Header.Get("X-Forwarded-Host")
		if tunnelHost == "" {
			return nil
		}

		// Rewrite Location header.
		if loc := resp.Header.Get("Location"); loc != "" {
			loc = strings.ReplaceAll(loc, "://"+domain, "://"+tunnelHost)
			if strings.HasPrefix(loc, "http://"+tunnelHost) {
				loc = "https://" + loc[len("http://"):]
			}
			resp.Header.Set("Location", loc)
		}

		// Rewrite body for text content (HTML/CSS/JS) so absolute asset URLs
		// pointing to the local .test domain are replaced with the tunnel host.
		// Skip encoded responses to avoid corrupting compressed data.
		ct := resp.Header.Get("Content-Type")
		enc := resp.Header.Get("Content-Encoding")
		if isTextContent(ct) && (enc == "" || enc == "identity") {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
			body = bytes.ReplaceAll(body, []byte("://"+domain), []byte("://"+tunnelHost))
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
		}

		return nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, err
	}

	srv := &http.Server{Handler: proxy}
	go srv.Serve(ln) //nolint:errcheck

	port := ln.Addr().(*net.TCPAddr).Port
	return port, func() { srv.Close() }, nil
}

// isTextContent returns true for Content-Type values whose body should be
// scanned and rewritten (HTML, CSS, JS, JSON).
func isTextContent(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "text/html") ||
		strings.Contains(ct, "text/css") ||
		strings.Contains(ct, "application/javascript") ||
		strings.Contains(ct, "text/javascript") ||
		strings.Contains(ct, "application/json")
}
