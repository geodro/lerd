package push

import (
	"os"
	"strings"
	"testing"
)

// withTempDataDir points config.DataDir at a fresh tmpdir for the test and
// restores XDG_DATA_HOME on cleanup. The cached VAPID keys are reset so the
// next VAPIDKeys() call re-reads from the new dir.
func withTempDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, hadPrev := os.LookupEnv("XDG_DATA_HOME")
	t.Setenv("XDG_DATA_HOME", dir)
	resetForTest()
	t.Cleanup(func() {
		if hadPrev {
			os.Setenv("XDG_DATA_HOME", prev)
		} else {
			os.Unsetenv("XDG_DATA_HOME")
		}
		resetForTest()
	})
	return dir
}

func TestVAPIDKeys_GeneratesAndPersists(t *testing.T) {
	withTempDataDir(t)

	priv, pub, err := VAPIDKeys()
	if err != nil {
		t.Fatalf("VAPIDKeys: %v", err)
	}
	if priv == "" || pub == "" {
		t.Fatalf("empty key pair: priv=%q pub=%q", priv, pub)
	}
	// Public key is base64url-encoded raw P-256 (65 bytes uncompressed),
	// which encodes to 87 chars without padding.
	if got := len(pub); got < 80 || got > 90 {
		t.Errorf("public key len = %d, want ~87", got)
	}
	if strings.Contains(pub, "+") || strings.Contains(pub, "/") {
		t.Errorf("public key contains non-url-safe chars: %s", pub)
	}

	priv2, pub2, err := VAPIDKeys()
	if err != nil {
		t.Fatalf("VAPIDKeys (second call): %v", err)
	}
	if priv2 != priv || pub2 != pub {
		t.Errorf("second call returned different keys: priv=%v pub=%v", priv2 != priv, pub2 != pub)
	}
}

func TestVAPIDKeys_ReloadsFromDisk(t *testing.T) {
	withTempDataDir(t)

	priv, pub, err := VAPIDKeys()
	if err != nil {
		t.Fatalf("VAPIDKeys: %v", err)
	}

	// Clear the in-memory cache so the next call has to read the persisted
	// files. The pair must match what was generated.
	resetForTest()
	priv2, pub2, err := VAPIDKeys()
	if err != nil {
		t.Fatalf("VAPIDKeys (post-reset): %v", err)
	}
	if priv2 != priv || pub2 != pub {
		t.Errorf("disk reload returned different keys")
	}
}

func TestVAPIDKeys_PrivateKeyFilePermissions(t *testing.T) {
	dir := withTempDataDir(t)
	if _, _, err := VAPIDKeys(); err != nil {
		t.Fatalf("VAPIDKeys: %v", err)
	}
	info, err := os.Stat(dir + "/lerd/" + vapidPrivateFile)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("private key perm = %o, want 0600", mode)
	}
}
