package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestHandleSettingsWorkerMode_UpdatesConfig is an integration test against
// the HTTP handler: POST a new mode and confirm the global config file
// reflects it. Uses a temp config dir so the test isn't destructive.
func TestHandleSettingsWorkerMode_UpdatesConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	body, _ := json.Marshal(map[string]string{"mode": config.WorkerExecModeContainer})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/worker-mode", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleSettingsWorkerMode(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, body %s", rec.Code, rec.Body.String())
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.WorkerExecMode(); got != config.WorkerExecModeContainer {
		t.Errorf("config not updated: got %q", got)
	}
}

func TestHandleSettingsWorkerMode_RejectsUnknownMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	body, _ := json.Marshal(map[string]string{"mode": "unknown"})
	req := httptest.NewRequest(http.MethodPost, "/api/settings/worker-mode", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handleSettingsWorkerMode(rec, req)
	if !strings.Contains(rec.Body.String(), "\"ok\":false") {
		t.Errorf("expected ok=false in response, got %s", rec.Body.String())
	}
}

func TestHandleSettingsWorkerMode_RejectsNonPOST(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/settings/worker-mode", nil)
	rec := httptest.NewRecorder()
	handleSettingsWorkerMode(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET should be rejected, got %d", rec.Code)
	}
}

func TestHandleSettings_IncludesWorkerMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()
	handleSettings(rec, req)

	var resp SettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if resp.WorkerExecMode == "" {
		t.Error("worker_exec_mode missing from response")
	}
}
