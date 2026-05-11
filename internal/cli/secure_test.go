package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateEnvAppURL must sync VITE_REVERB_HOST/SCHEME/PORT alongside APP_URL.
// Vite bakes these into built JS, so a secure flip that only touches APP_URL
// leaves the browser-side WebSocket pointing at wss://host:80 (or ws on 443)
// and Echo can't connect.
func TestUpdateEnvAppURL_syncsViteReverbVars(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	stale := "APP_URL=http://acme.test\n" +
		"VITE_REVERB_HOST=acme.test\n" +
		"VITE_REVERB_SCHEME=http\n" +
		"VITE_REVERB_PORT=80\n"
	if err := os.WriteFile(envPath, []byte(stale), 0644); err != nil {
		t.Fatal(err)
	}

	updateEnvAppURL(dir, "https", "acme.test")

	got, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	checks := map[string]string{
		"APP_URL":            "APP_URL=https://acme.test",
		"VITE_REVERB_HOST":   "VITE_REVERB_HOST=acme.test",
		"VITE_REVERB_SCHEME": "VITE_REVERB_SCHEME=https",
		"VITE_REVERB_PORT":   "VITE_REVERB_PORT=443",
	}
	for key, want := range checks {
		if !strings.Contains(s, want) {
			t.Errorf("%s not synced; want %q in:\n%s", key, want, s)
		}
	}
}

// Inverse: unsecuring must walk the same triplet back to ws/80.
func TestUpdateEnvAppURL_unsecureWalksViteReverbBack(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	current := "APP_URL=https://acme.test\n" +
		"VITE_REVERB_HOST=acme.test\n" +
		"VITE_REVERB_SCHEME=https\n" +
		"VITE_REVERB_PORT=443\n"
	if err := os.WriteFile(envPath, []byte(current), 0644); err != nil {
		t.Fatal(err)
	}

	updateEnvAppURL(dir, "http", "acme.test")

	got, _ := os.ReadFile(envPath)
	s := string(got)
	if !strings.Contains(s, "VITE_REVERB_SCHEME=http") {
		t.Errorf("VITE_REVERB_SCHEME not flipped to http:\n%s", s)
	}
	if !strings.Contains(s, "VITE_REVERB_PORT=80") {
		t.Errorf("VITE_REVERB_PORT not flipped to 80:\n%s", s)
	}
}
