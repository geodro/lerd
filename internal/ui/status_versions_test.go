package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// The site PHP dropdown filters FrankenPHP sites against this field, so the
// status payload must expose the FrankenPHP-publishable set under the exact
// json tag the frontend reads.
func TestStatusResponseFrankenPHPVersionsTag(t *testing.T) {
	b, err := json.Marshal(StatusResponse{FrankenPHPVersions: config.FrankenPHPVersions()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"frankenphp_php_versions":["8.2","8.3","8.4","8.5"]`) {
		t.Errorf("status JSON missing expected frankenphp_php_versions: %s", b)
	}
}
