package nginx

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/geodro/lerd/internal/config"
)

// trustTokenFile is the on-disk location of the per-install random token used
// by nginx to authenticate proxied requests from the lerd.localhost vhost
// back to the lerd-ui process. Mode 0600 in the user's data dir means a LAN
// attacker cannot read it (filesystem isolation), and the token is rotated
// by deleting the file and re-running `lerd install`.
//
// The threat model: lerd-nginx runs in a rootless podman bridge, so its
// outbound connections to host services arrive at lerd-ui with a non-loopback
// source IP (the bridge gateway). Without this token, lerd-ui couldn't tell
// a legitimate proxy hop from the lerd.localhost vhost apart from a LAN
// attacker hitting lerd-ui directly via http://server-ip:7073. With the
// token, nginx's `proxy_set_header X-Lerd-Trust ...` clobbers any
// client-supplied value, so a LAN attacker cannot inject the header by
// setting it on their own request — nginx overwrites their value with the
// real one before proxying. The only way the header reaches lerd-ui with a
// valid value is via the lerd.localhost vhost.
const trustTokenFile = "nginx-trust-token"

var (
	cachedTrustToken string
	trustTokenMu     sync.Mutex
)

// TrustTokenPath returns the absolute filesystem path of the trust token file.
func TrustTokenPath() string {
	return filepath.Join(config.DataDir(), trustTokenFile)
}

// LoadOrGenerateTrustToken returns the per-install nginx → lerd-ui trust
// token, generating a fresh 32-byte hex value on first call and persisting
// it to ~/.local/share/lerd/nginx-trust-token (mode 0600). Subsequent calls
// return the cached value so the file is read at most once per process.
//
// Idempotent across processes: if two lerd processes race on first
// generation, the second one's write loses but both end up with a valid
// token because the read-after-write resolves the race.
func LoadOrGenerateTrustToken() (string, error) {
	trustTokenMu.Lock()
	defer trustTokenMu.Unlock()
	if cachedTrustToken != "" {
		return cachedTrustToken, nil
	}

	path := TrustTokenPath()
	if data, err := os.ReadFile(path); err == nil {
		token := strings.TrimSpace(string(data))
		if token != "" {
			cachedTrustToken = token
			return token, nil
		}
	}

	// Generate. 32 bytes → 64 hex chars; brute-forcing this header value
	// from the LAN at HTTP request rates is infeasible.
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating trust token: %w", err)
	}
	token := hex.EncodeToString(buf)

	if err := os.MkdirAll(config.DataDir(), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("writing trust token: %w", err)
	}
	cachedTrustToken = token
	return token, nil
}
