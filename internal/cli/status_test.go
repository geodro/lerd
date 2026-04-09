package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// captureStdout runs fn with os.Stdout redirected into a buffer and returns
// everything fn wrote. Used to assert on the text printed by the [Remote Access]
// section of `lerd status`.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	return <-done
}

func TestPrintRemoteAccessStatus(t *testing.T) {
	cases := []struct {
		name       string
		exposed    bool
		lanIP      string
		username   string
		passHash   string
		wantSubstr []string
	}{
		{
			name:    "both off",
			exposed: false,
			wantSubstr: []string{
				"LAN exposure",
				"loopback only",
				"lerd lan expose",
				"Dashboard remote access",
				"LAN clients get 403",
				"lerd remote-control on",
			},
		},
		{
			name:    "lan exposed, dashboard off",
			exposed: true,
			lanIP:   "192.168.1.42",
			wantSubstr: []string{
				"LAN exposure (192.168.1.42)",
				"OK",
				"Dashboard remote access",
				"LAN clients get 403",
			},
		},
		{
			name:     "both on",
			exposed:  true,
			lanIP:    "10.0.0.5",
			username: "george",
			passHash: "$2a$10$fakehashfakehashfakehashfakehashfakehashfakehashfake",
			wantSubstr: []string{
				"LAN exposure (10.0.0.5)",
				"Dashboard remote access (user: george)",
			},
		},
		{
			name:    "lan exposed with unknown ip",
			exposed: true,
			lanIP:   "",
			wantSubstr: []string{
				"LAN exposure ((unknown))",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.GlobalConfig{}
			cfg.LAN.Exposed = tc.exposed
			cfg.UI.Username = tc.username
			cfg.UI.PasswordHash = tc.passHash

			out := captureStdout(t, func() {
				printRemoteAccessStatus(cfg, tc.lanIP)
			})

			for _, want := range tc.wantSubstr {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\nfull output:\n%s", want, out)
				}
			}
		})
	}
}
