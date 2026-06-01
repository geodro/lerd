//go:build darwin

package cli

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestVirtiofsTag(t *testing.T) {
	// These expected tags were verified against a real Podman 5.x
	// machine config. If this test breaks, Podman changed its tag
	// algorithm and ensurePodmanMachineMounts must be updated.
	cases := map[string]string{
		"/Volumes":     "38bafbaf0d295e2feac49d098a18f9974b85",
		"/private":     "71708eb255bc230cd7c91dd26f7667a7b938",
		"/var/folders": "a0bb3a2c8b0b02ba5958b0576f0d6530e104",
		"/Users":       "a2a0ee2c717462feb1de2f5afd59de5fd2d8",
	}
	for path, want := range cases {
		hash := sha256.Sum256([]byte(path))
		got := fmt.Sprintf("%x", hash)[:36]
		if got != want {
			t.Errorf("virtiofs tag for %q: got %s, want %s", path, got, want)
		}
	}
}
