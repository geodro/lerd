package dns

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// When dns.enabled is false, Check must short-circuit to OK without resolving.
// The whole point of disabled mode is that lerd does not own resolution, so
// probing would either succeed accidentally (RFC 6761 *.localhost) or fail in
// a way that is not actionable.
func TestCheck_DNSDisabledReturnsOK(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg := &config.GlobalConfig{}
	cfg.DNS.Enabled = false
	cfg.DNS.TLD = "localhost"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	ok, err := Check("test")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !ok {
		t.Fatalf("Check should return OK when DNS disabled, got false")
	}
}

// CheckStatus must short-circuit to StatusOK in disabled mode for the same
// reason Check does: lerd does not own resolution there, so probing would
// be misleading rather than actionable.
func TestCheckStatus_DNSDisabledReturnsOK(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg := &config.GlobalConfig{}
	cfg.DNS.Enabled = false
	cfg.DNS.TLD = "localhost"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	if got := CheckStatus("test"); got != StatusOK {
		t.Fatalf("CheckStatus = %q, want %q when DNS disabled", got, StatusOK)
	}
}
