package certs

import (
	"errors"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestWorktreeCertDomains_appendsWorktreeDomains(t *testing.T) {
	site := []string{"myapp.test"}
	wt := []string{"feat-x.myapp.test", "feat-y.myapp.test"}

	got := WorktreeCertDomains(site, wt)

	want := []string{"myapp.test", "feat-x.myapp.test", "feat-y.myapp.test"}
	if len(got) != len(want) {
		t.Fatalf("got %d domains, want %d: %v", len(got), len(want), got)
	}
	for i, d := range want {
		if got[i] != d {
			t.Errorf("domain[%d] = %q, want %q", i, got[i], d)
		}
	}
}

func TestWorktreeCertDomains_noWorktrees(t *testing.T) {
	got := WorktreeCertDomains([]string{"myapp.test"}, nil)
	if len(got) != 1 || got[0] != "myapp.test" {
		t.Errorf("expected only site domain, got: %v", got)
	}
}

func TestWorktreeCertDomains_doesNotMutateInput(t *testing.T) {
	site := []string{"myapp.test"}
	_ = WorktreeCertDomains(site, []string{"feat.myapp.test"})
	if len(site) != 1 {
		t.Error("WorktreeCertDomains mutated the input slice")
	}
}

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
