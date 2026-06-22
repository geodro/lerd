package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// writeDNSDisabledConfig writes a global config with DNS management off so the
// upstream handler persists the value but skips the dnsmasq rewrite + restart,
// which needs a running systemd/podman that the test environment lacks.
func writeDNSDisabledConfig(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "lerd")
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(dir))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("dns:\n  enabled: false\n  tld: test\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestHandleSettingsDNSUpstream_PersistsCleanedList(t *testing.T) {
	writeDNSDisabledConfig(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	body, _ := json.Marshal(map[string][]string{"upstream": {" 192.168.100.129 ", "1.1.1.1#5353", ""}})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/dns-upstream", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handleSettingsDNSUpstream(rec, req)

	if !strings.Contains(rec.Body.String(), "\"ok\":true") {
		t.Fatalf("expected ok=true, got %s", rec.Body.String())
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.168.100.129", "1.1.1.1#5353"}
	if len(cfg.DNS.Upstream) != len(want) {
		t.Fatalf("got %v, want %v", cfg.DNS.Upstream, want)
	}
	for i := range want {
		if cfg.DNS.Upstream[i] != want[i] {
			t.Errorf("index %d: got %q want %q", i, cfg.DNS.Upstream[i], want[i])
		}
	}
}

func TestHandleSettingsDNSUpstream_RejectsInvalidEntry(t *testing.T) {
	writeDNSDisabledConfig(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	body, _ := json.Marshal(map[string][]string{"upstream": {"not-an-ip"}})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/dns-upstream", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handleSettingsDNSUpstream(rec, req)

	if !strings.Contains(rec.Body.String(), "\"ok\":false") {
		t.Errorf("expected ok=false, got %s", rec.Body.String())
	}
	// The bad value must not have been persisted.
	cfg, _ := config.LoadGlobal()
	if len(cfg.DNS.Upstream) != 0 {
		t.Errorf("invalid entry should not persist, got %v", cfg.DNS.Upstream)
	}
}

func TestHandleSettingsDNSUpstream_ClearsWhenEmpty(t *testing.T) {
	writeDNSDisabledConfig(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cfg, _ := config.LoadGlobal()
	cfg.DNS.Upstream = []string{"192.168.100.129"}
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string][]string{"upstream": {}})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/dns-upstream", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handleSettingsDNSUpstream(rec, req)

	if !strings.Contains(rec.Body.String(), "\"ok\":true") {
		t.Fatalf("expected ok=true, got %s", rec.Body.String())
	}
	reloaded, _ := config.LoadGlobal()
	if len(reloaded.DNS.Upstream) != 0 {
		t.Errorf("expected upstream cleared, got %v", reloaded.DNS.Upstream)
	}
}

func TestHandleSettingsDNSUpstream_RejectsNonPOST(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/settings/dns-upstream", nil)
	rec := httptest.NewRecorder()
	handleSettingsDNSUpstream(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET should be rejected, got %d", rec.Code)
	}
}
