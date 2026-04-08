package ui

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/cli"
	"github.com/geodro/lerd/internal/config"
)

// handleRemoteSetup serves the GET /api/remote-setup endpoint that hands a
// remote device a self-contained bash script for laptop provisioning. The
// script embeds the mkcert root CA (public only) as a base64 heredoc, has
// the server's auto-detected LAN IP filled in, and runs platform-specific
// resolver setup so .test domains resolve from the laptop without a
// per-site /etc/hosts entry.
//
// Access is gated two ways:
//
//  1. Source IP must be in an RFC 1918 private range (or loopback). Public
//     internet requests are rejected with 403 even if the dashboard happens
//     to be exposed beyond the LAN, so a misconfigured firewall can't leak
//     the trust anchor to the world.
//  2. Query parameter `code=<token>` must match the active token persisted
//     by `lerd remote-setup token`, and that token must not be expired. The
//     token is single-use — successful validation deletes it so the same
//     code can't be replayed.
func handleRemoteSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Token gate. The endpoint is *opt-in*: when no token has been
	// generated on the server it is invisible (404) so a network scanner
	// can't distinguish "endpoint disabled" from "endpoint not present".
	// Token check happens before the source-IP check so a public-internet
	// probe gets the same 404 as "no such route" — no information leak.
	token, err := cli.LoadRemoteSetupToken()
	if err != nil {
		http.Error(w, "loading token: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if token == nil {
		http.NotFound(w, r)
		return
	}
	if time.Now().After(token.Expires) {
		_ = cli.ClearRemoteSetupToken()
		http.NotFound(w, r)
		return
	}

	// 2. Source IP gate. Once a token is active, an out-of-LAN caller still
	// gets a clear 403 so the legitimate user can diagnose a misconfigured
	// VPN or firewall.
	clientIP := remoteSetupClientIP(r)
	if !isPrivateIP(clientIP) {
		http.Error(w, "remote-setup is only available from private LAN addresses", http.StatusForbidden)
		return
	}

	// 3. Code check. Wrong codes increment a per-token failure counter and
	// wipe the token after MaxRemoteSetupFailures attempts so a brute-force
	// scan against the LAN endpoint terminates after a fixed budget.
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "missing code query parameter", http.StatusUnauthorized)
		return
	}
	if !subtleEqual(token.Token, code) {
		closed, recErr := cli.RecordRemoteSetupFailure()
		if recErr != nil {
			http.Error(w, "recording failure: "+recErr.Error(), http.StatusInternalServerError)
			return
		}
		if closed {
			http.Error(w, fmt.Sprintf("invalid code — too many wrong attempts, code revoked. Generate a new one with `lerd remote-setup`."), http.StatusUnauthorized)
			return
		}
		http.Error(w, "invalid code", http.StatusUnauthorized)
		return
	}

	// Single-use: revoke immediately on successful match.
	_ = cli.ClearRemoteSetupToken()

	// 3. Build the response script
	script, err := buildRemoteSetupScript(r)
	if err != nil {
		http.Error(w, "building script: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(script))
}

// remoteSetupClientIP extracts the client IP from the request, stripping any
// port number. We don't honor X-Forwarded-For — this endpoint is meant to be
// hit directly on the LAN, not through a reverse proxy.
func remoteSetupClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isPrivateIP reports whether the given address is in an RFC 1918 private
// range, link-local, or loopback. Used as a defense-in-depth check on the
// remote-setup endpoint so accidentally public dashboards don't hand out
// the mkcert CA to the internet.
func isPrivateIP(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return true
	}
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"} {
		_, block, _ := net.ParseCIDR(cidr)
		if block != nil && block.Contains(ip) {
			return true
		}
	}
	return false
}

// subtleEqual is a constant-time string comparison to avoid timing attacks
// on the token check. Stdlib's subtle.ConstantTimeCompare requires equal
// lengths, so we wrap that.
func subtleEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// buildRemoteSetupScript renders the bash script that the laptop pipes into
// `bash`. The script is generated per-request so it can embed the live mkcert
// root CA and the server's current LAN IP without writing them to disk.
func buildRemoteSetupScript(r *http.Request) (string, error) {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return "", err
	}
	tld := cfg.DNS.TLD
	if tld == "" {
		tld = "test"
	}

	serverIP, err := detectPrimaryLANIP()
	if err != nil {
		return "", fmt.Errorf("could not detect server LAN IP: %w", err)
	}

	caRoot, err := mkcertCAROOT()
	if err != nil {
		return "", fmt.Errorf("locating mkcert CA: %w", err)
	}
	caBytes, err := os.ReadFile(filepath.Join(caRoot, "rootCA.pem"))
	if err != nil {
		return "", fmt.Errorf("reading mkcert root CA: %w", err)
	}
	caB64 := base64.StdEncoding.EncodeToString(caBytes)

	return fmt.Sprintf(remoteSetupScriptTemplate, serverIP, tld, caB64), nil
}

// detectPrimaryLANIP duplicates the logic in cli/dns.go (importing cli from
// ui would create a cycle later if cli ever needed something from ui). The
// dial trick discovers which interface the kernel would route a public
// destination through, without sending any packets.
func detectPrimaryLANIP() (string, error) {
	conn, err := net.Dial("udp4", "1.1.1.1:80")
	if err == nil {
		defer conn.Close()
		return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
	}
	ifaces, ifErr := net.Interfaces()
	if ifErr != nil {
		return "", fmt.Errorf("listing interfaces: %w", ifErr)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if v4 := ipnet.IP.To4(); v4 != nil && !v4.IsLoopback() {
					return v4.String(), nil
				}
			}
		}
	}
	return "", fmt.Errorf("no usable IPv4 address found")
}

// mkcertCAROOT runs `mkcert -CAROOT` to find where the root CA lives.
func mkcertCAROOT() (string, error) {
	bin := certs.MkcertPath()
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("mkcert binary not found at %s — run `lerd install` first", bin)
	}
	out, err := exec.Command(bin, "-CAROOT").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// remoteSetupScriptTemplate is a bash script with three printf placeholders:
// %s for the server LAN IP, %s for the TLD, %s for the base64-encoded CA.
const remoteSetupScriptTemplate = `#!/usr/bin/env bash
# lerd remote setup — generated by /api/remote-setup
# Sets up the calling machine to resolve *.%[2]s against this lerd server
# and to trust its mkcert root CA so HTTPS works without warnings.

set -euo pipefail

SERVER_IP="%[1]s"
TLD="%[2]s"
CA_B64="%[3]s"

step() { printf "\n\033[1;36m→ %%s\033[0m\n" "$*"; }
ok()   { printf "  \033[32m✓\033[0m %%s\n" "$*"; }
warn() { printf "  \033[33m!\033[0m %%s\n" "$*"; }
err()  { printf "  \033[31m✗\033[0m %%s\n" "$*" >&2; }

OS_KIND="unknown"
case "$(uname -s)" in
  Linux*)  OS_KIND="linux" ;;
  Darwin*) OS_KIND="macos" ;;
esac

# ── 1. install mkcert if missing ─────────────────────────────────────────
step "Ensuring mkcert is installed"
if command -v mkcert >/dev/null 2>&1; then
  ok "mkcert already installed"
else
  warn "mkcert not installed — attempting to install"
  case "$OS_KIND" in
    linux)
      if command -v apt-get >/dev/null 2>&1; then
        sudo apt-get install -y libnss3-tools mkcert
      elif command -v dnf >/dev/null 2>&1; then
        sudo dnf install -y nss-tools mkcert
      elif command -v pacman >/dev/null 2>&1; then
        sudo pacman -S --noconfirm nss mkcert
      else
        err "No supported package manager found. Install mkcert manually and re-run."
        exit 1
      fi
      ;;
    macos)
      if ! command -v brew >/dev/null 2>&1; then
        err "Homebrew not found. Install Homebrew or mkcert manually."
        exit 1
      fi
      brew install mkcert nss
      ;;
    *)
      err "Unsupported OS for automatic mkcert install."
      exit 1
      ;;
  esac
  ok "mkcert installed"
fi

# ── 2. install the lerd root CA ──────────────────────────────────────────
step "Installing the lerd root CA into the system trust store"
CAROOT="$(mkcert -CAROOT)"
mkdir -p "$CAROOT"
echo "$CA_B64" | base64 --decode > "$CAROOT/rootCA.pem"
mkcert -install
ok "lerd root CA trusted"

# ── 3. configure DNS forwarding ──────────────────────────────────────────
step "Configuring local resolver to forward .$TLD to $SERVER_IP"
case "$OS_KIND" in
  linux)
    NM_DNSMASQ_DIR="/etc/NetworkManager/dnsmasq.d"
    DNSMASQ_DROPIN="/etc/dnsmasq.d/lerd.conf"
    RESOLVED_DROPIN="/etc/systemd/resolved.conf.d/lerd-test.conf"
    LINE="server=/$TLD/$SERVER_IP#5300"

    # Check whether NetworkManager is actually configured to use the
    # dnsmasq plugin (just having /etc/NetworkManager/dnsmasq.d/ exist is
    # not enough — that directory ships with the package even when NM
    # uses systemd-resolved or another backend).
    nm_uses_dnsmasq=0
    if grep -rqs '^dns=dnsmasq' /etc/NetworkManager/NetworkManager.conf /etc/NetworkManager/conf.d/ 2>/dev/null; then
      nm_uses_dnsmasq=1
    fi

    if [ "$nm_uses_dnsmasq" = "1" ]; then
      sudo mkdir -p "$NM_DNSMASQ_DIR"
      echo "$LINE" | sudo tee "$NM_DNSMASQ_DIR/lerd.conf" >/dev/null
      sudo systemctl restart NetworkManager
      ok "Wrote $NM_DNSMASQ_DIR/lerd.conf and restarted NetworkManager"
    elif systemctl is-active systemd-resolved >/dev/null 2>&1; then
      # systemd-resolved is the active resolver. Use a per-domain dropin
      # that forwards .test queries to the lerd server. Requires systemd
      # 254+ for the host:port syntax in DNS=. Older systemd will fail
      # the restart and we fall through to the manual instructions.
      sudo mkdir -p "$(dirname "$RESOLVED_DROPIN")"
      sudo tee "$RESOLVED_DROPIN" >/dev/null <<DROPIN
[Resolve]
DNS=$SERVER_IP:5300
Domains=~$TLD
DROPIN
      if sudo systemctl restart systemd-resolved 2>/dev/null; then
        ok "Wrote $RESOLVED_DROPIN and restarted systemd-resolved"
      else
        sudo rm -f "$RESOLVED_DROPIN"
        err "systemd-resolved restart failed (likely systemd < 254 which does not support DNS=host:port)."
        err "Install dnsmasq locally and configure systemd-resolved to forward to it,"
        err "or add an /etc/hosts entry per site you need to access remotely."
        exit 1
      fi
    elif command -v dnsmasq >/dev/null 2>&1 && [ -d /etc/dnsmasq.d ]; then
      echo "$LINE" | sudo tee "$DNSMASQ_DROPIN" >/dev/null
      sudo systemctl restart dnsmasq 2>/dev/null || true
      ok "Wrote $DNSMASQ_DROPIN and reloaded dnsmasq"
    else
      err "Could not detect a supported resolver."
      err "Manually add to /etc/hosts:  $SERVER_IP <hostname>.$TLD"
      err "Or install dnsmasq locally and add: $LINE"
      exit 1
    fi
    ;;
  macos)
    sudo mkdir -p /etc/resolver
    printf "nameserver %%s\nport 5300\n" "$SERVER_IP" | sudo tee "/etc/resolver/$TLD" >/dev/null
    ok "Wrote /etc/resolver/$TLD — macOS resolver picks this up automatically"
    ;;
  *)
    err "Unsupported OS for resolver auto-configuration."
    exit 1
    ;;
esac

# ── 4. done ──────────────────────────────────────────────────────────────
cat <<EOF

Setup complete. Open the lerd dashboard from this device at:
  http://$SERVER_IP:7073

Verify a .$TLD hostname resolves:
  dig myapp.$TLD               # may need a moment for the resolver cache to clear

⚠  Server IP is hard-coded to $SERVER_IP in the resolver dropin.
   If the lerd server later moves to a different network and gets a
   new LAN IP, .$TLD lookups from this machine will start failing.
   To fix it, run 'lerd remote-setup' on the server again, then re-run
   the new curl one-liner from this machine — it overwrites the dropin
   in place. No need to revert anything first.

To revert later:
  • Linux NM:        sudo rm /etc/NetworkManager/dnsmasq.d/lerd.conf && sudo systemctl restart NetworkManager
  • Linux dnsmasq:   sudo rm /etc/dnsmasq.d/lerd.conf && sudo systemctl restart dnsmasq
  • Linux resolved: sudo rm /etc/systemd/resolved.conf.d/lerd-test.conf && sudo systemctl restart systemd-resolved
  • macOS:           sudo rm /etc/resolver/$TLD
  • Cert:            mkcert -uninstall && rm "$CAROOT/rootCA.pem"
EOF
`
