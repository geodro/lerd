package certs

import (
	"errors"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestSecureSite_RefusesWhenDNSDisabled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	cfg := &config.GlobalConfig{}
	cfg.DNS.Enabled = false
	cfg.DNS.TLD = "localhost"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	site := config.Site{Name: "myapp", Domains: []string{"myapp.localhost"}}
	err := SecureSite(site)
	if !errors.Is(err, ErrDNSDisabled) {
		t.Fatalf("SecureSite err = %v, want ErrDNSDisabled", err)
	}
}
