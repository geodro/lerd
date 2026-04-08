package cli

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
)

// RemoteSetupToken is the on-disk representation of a one-time code that
// authorizes a remote device to call the /api/remote-setup endpoint.
//
// Stored in <DataDir>/remote-setup-token.json with mode 0600. The token is
// short (8 characters), high-entropy enough for the LAN-only threat model,
// gated by a TTL so a forgotten token doesn't grant indefinite access, and
// counted against a per-token failure limit so brute-force attempts close
// the endpoint instead of running indefinitely.
type RemoteSetupToken struct {
	Token    string    `json:"token"`
	Expires  time.Time `json:"expires"`
	Failures int       `json:"failures,omitempty"`
}

// MaxRemoteSetupFailures is the number of wrong-code attempts that wipe the
// active token and force the user to generate a fresh one. The token's
// 8-character alphabet has ~55^8 ≈ 8 × 10^13 possible values, so 10 attempts
// is effectively zero brute-force progress — this lock is defense in depth,
// not a tight bound.
const MaxRemoteSetupFailures = 10

// remoteSetupTokenPath returns the path to the on-disk token file.
func remoteSetupTokenPath() string {
	return filepath.Join(config.DataDir(), "remote-setup-token.json")
}

// LoadRemoteSetupToken reads the current token from disk. Returns nil
// (no error) when the file doesn't exist — i.e. no token is active.
func LoadRemoteSetupToken() (*RemoteSetupToken, error) {
	data, err := os.ReadFile(remoteSetupTokenPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var t RemoteSetupToken
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parsing token file: %w", err)
	}
	return &t, nil
}

// SaveRemoteSetupToken writes the token to disk with mode 0600.
func SaveRemoteSetupToken(t *RemoteSetupToken) error {
	if err := os.MkdirAll(config.DataDir(), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return os.WriteFile(remoteSetupTokenPath(), data, 0600)
}

// ClearRemoteSetupToken removes the on-disk token file.
func ClearRemoteSetupToken() error {
	err := os.Remove(remoteSetupTokenPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RecordRemoteSetupFailure increments the failure counter on the active
// token. When the count reaches MaxRemoteSetupFailures the token is wiped
// from disk and `closed` is true — subsequent calls to the endpoint return
// 404 until the user generates a new token. Idempotent against concurrent
// callers in the worst case (one extra increment).
func RecordRemoteSetupFailure() (closed bool, err error) {
	t, err := LoadRemoteSetupToken()
	if err != nil {
		return false, err
	}
	if t == nil {
		return true, nil
	}
	t.Failures++
	if t.Failures >= MaxRemoteSetupFailures {
		if err := ClearRemoteSetupToken(); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := SaveRemoteSetupToken(t); err != nil {
		return false, err
	}
	return false, nil
}

// GenerateRemoteSetupToken creates a fresh one-time setup code, persists it
// with the given TTL, and returns the code. Used by both the `lerd
// remote-setup` cobra command and the dashboard UI's loopback-only generate
// endpoint, so they share the exact same token format and storage path.
func GenerateRemoteSetupToken(ttl time.Duration) (string, error) {
	code, err := generateRemoteSetupCode()
	if err != nil {
		return "", fmt.Errorf("generating code: %w", err)
	}
	token := &RemoteSetupToken{
		Token:   code,
		Expires: time.Now().Add(ttl),
	}
	if err := SaveRemoteSetupToken(token); err != nil {
		return "", fmt.Errorf("saving token: %w", err)
	}
	return code, nil
}

// generateRemoteSetupCode returns an 8-character random alphanumeric string
// suitable as a one-time code for the /api/remote-setup endpoint. Uses
// crypto/rand so it can't be predicted by a network observer guessing seeds.
func generateRemoteSetupCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789" // no 0/O/1/I/l confusables
	const length = 8
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

// NewRemoteSetupCmd returns the `lerd remote-setup` command — generates a
// one-time code that authorizes a single laptop to call /api/remote-setup
// and provision itself against this lerd instance.
func NewRemoteSetupCmd() *cobra.Command {
	var ttl time.Duration
	var revoke bool
	cmd := &cobra.Command{
		Use:   "remote-setup",
		Short: "Generate a one-time code that lets a laptop provision itself against this lerd server",
		Long: `Generates an 8-character one-time code and prints the curl one-liner the
laptop should run to set itself up against this lerd server. Auto-enables
'lerd lan:expose' if it isn't already active — exposing lerd to the LAN
is required for the remote device to reach the sites and resolve .test
hostnames.

The /api/remote-setup endpoint on the lerd dashboard server is gated by:
  • the code being present and not expired (default TTL 15 minutes)
  • the source IP being in an RFC 1918 private range (10/8, 172.16/12, 192.168/16)

The code is single-use — it is consumed on the first successful call.
Re-run this command to generate a new code if it expires or is consumed.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if revoke {
				if err := ClearRemoteSetupToken(); err != nil {
					return fmt.Errorf("revoking token: %w", err)
				}
				fmt.Println("Remote setup token revoked.")
				return nil
			}

			// Always (re)apply LAN exposure. EnableLANExposure is idempotent
			// and reapplying it heals any state drift between cfg.LAN.Exposed
			// and the actual on-disk container quadlets / forwarder unit /
			// dnsmasq config / lerd-ui bind. The whole point of generating
			// a setup code is to provision a remote device, which can't
			// work unless lerd is reachable from the LAN.
			fmt.Println("Ensuring lerd is exposed on the LAN...")
			lanIP, err := EnableLANExposure(func(step string) {
				fmt.Printf("  • %s\n", step)
			})
			if err != nil {
				return fmt.Errorf("enabling lan:expose: %w", err)
			}
			fmt.Printf("  lerd-dns-forwarder running on %s:5300 (UDP+TCP), answering *.test → %s\n", lanIP, lanIP)
			fmt.Println("  Make sure your firewall allows port 5300 from the devices you want to grant access.")

			code, err := GenerateRemoteSetupToken(ttl)
			if err != nil {
				return err
			}

			lanIP, ipErr := detectPrimaryLANIP()
			fmt.Println()
			fmt.Printf("  Code: %s\n", code)
			fmt.Printf("  Expires in: %s\n", ttl)
			fmt.Println()
			fmt.Println("On the laptop, run:")
			fmt.Println()
			if ipErr == nil {
				fmt.Printf("  curl -sSL 'http://%s:7073/api/remote-setup?code=%s' | bash\n", lanIP, code)
			} else {
				fmt.Printf("  curl -sSL 'http://<server-ip>:7073/api/remote-setup?code=%s' | bash\n", code)
				fmt.Fprintln(os.Stderr, "warning: could not auto-detect a LAN IP — substitute the server's address yourself")
			}
			fmt.Println()
			fmt.Println("The endpoint is restricted to RFC 1918 private source IPs and the code")
			fmt.Println("is single-use. Re-run this command to generate a new one if it expires.")
			return nil
		},
	}
	cmd.Flags().DurationVar(&ttl, "ttl", 15*time.Minute, "how long the code is valid for")
	cmd.Flags().BoolVar(&revoke, "revoke", false, "delete the active code without generating a new one")
	return cmd
}
