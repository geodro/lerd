//go:build linux

package dns

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- parseNmcliOutput ---

func TestParseNmcliOutput_basic(t *testing.T) {
	input := "192.168.1.1\n8.8.8.8\n\n"
	got := parseNmcliLines(input)
	want := []string{"192.168.1.1", "8.8.8.8"}
	assertSliceEqual(t, got, want)
}

func TestParseNmcliOutput_pipeSeparated(t *testing.T) {
	input := "192.168.1.1|8.8.8.8\n"
	got := parseNmcliLines(input)
	want := []string{"192.168.1.1", "8.8.8.8"}
	assertSliceEqual(t, got, want)
}

func TestParseNmcliOutput_skipsLoopbackAndDash(t *testing.T) {
	input := "127.0.0.53\n--\n\n10.0.0.1\n127.0.0.1\n"
	got := parseNmcliLines(input)
	want := []string{"10.0.0.1"}
	assertSliceEqual(t, got, want)
}

func TestParseNmcliOutput_deduplicates(t *testing.T) {
	input := "8.8.8.8\n8.8.8.8\n8.8.4.4\n"
	got := parseNmcliLines(input)
	want := []string{"8.8.8.8", "8.8.4.4"}
	assertSliceEqual(t, got, want)
}

func TestParseNmcliOutput_empty(t *testing.T) {
	got := parseNmcliLines("")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// --- parseDefaultInterface ---

func TestParseDefaultInterface_typical(t *testing.T) {
	input := "default via 192.168.1.1 dev enp1s0 proto dhcp src 192.168.1.100 metric 100"
	got := parseDefaultIface(input)
	if got != "enp1s0" {
		t.Errorf("expected enp1s0, got %q", got)
	}
}

func TestParseDefaultInterface_wifi(t *testing.T) {
	input := "default via 10.0.0.1 dev wlp2s0 proto dhcp metric 600"
	got := parseDefaultIface(input)
	if got != "wlp2s0" {
		t.Errorf("expected wlp2s0, got %q", got)
	}
}

func TestParseDefaultInterface_multipleRoutes(t *testing.T) {
	input := "default via 192.168.1.1 dev eth0 proto dhcp\ndefault via 10.0.0.1 dev eth1 proto static"
	got := parseDefaultIface(input)
	if got != "eth0" {
		t.Errorf("expected eth0, got %q", got)
	}
}

func TestParseDefaultInterface_empty(t *testing.T) {
	got := parseDefaultIface("")
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// --- WriteDnsmasqConfig ---

func TestWriteDnsmasqConfig_withUpstreams(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := t.TempDir()
	fakeResolv := writeTempFile(t, "nameserver 192.168.1.1\nnameserver 8.8.8.8\n")
	origPaths := resolvPaths
	resolvPaths = []string{fakeResolv}
	defer func() { resolvPaths = origPaths }()

	if err := WriteDnsmasqConfig(dir); err != nil {
		t.Fatalf("WriteDnsmasqConfig: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "lerd.conf"))

	assertContains(t, content, "port=5300")
	assertContains(t, content, "no-resolv")
	assertContains(t, content, "server=192.168.1.1")
	assertContains(t, content, "server=8.8.8.8")
	assertContains(t, content, "address=/.test/127.0.0.1")
}

func TestWriteDnsmasqConfig_noUpstreams(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir := t.TempDir()
	fakeResolv := writeTempFile(t, "nameserver 127.0.0.53\n")
	origPaths := resolvPaths
	origNmcli := nmcliDNSFunc
	resolvPaths = []string{fakeResolv}
	nmcliDNSFunc = func() []string { return nil }
	defer func() {
		resolvPaths = origPaths
		nmcliDNSFunc = origNmcli
	}()

	if err := WriteDnsmasqConfig(dir); err != nil {
		t.Fatalf("WriteDnsmasqConfig: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "lerd.conf"))

	assertContains(t, content, "port=5300")
	assertContains(t, content, "address=/.test/127.0.0.1")
	if strings.Contains(content, "no-resolv") {
		t.Error("expected no-resolv to be absent when no upstreams detected")
	}
}

// --- WriteDnsmasqConfigFor ---

func TestWriteDnsmasqConfigFor_customTarget(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	fakeResolv := writeTempFile(t, "nameserver 127.0.0.53\n")
	origPaths := resolvPaths
	origNmcli := nmcliDNSFunc
	resolvPaths = []string{fakeResolv}
	nmcliDNSFunc = func() []string { return nil }
	defer func() { resolvPaths = origPaths; nmcliDNSFunc = origNmcli }()

	if err := WriteDnsmasqConfigFor(dir, "10.0.0.5"); err != nil {
		t.Fatalf("WriteDnsmasqConfigFor: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "lerd.conf"))
	assertContains(t, content, "address=/.test/10.0.0.5")
	if strings.Contains(content, "no-resolv") {
		t.Error("expected no-resolv absent when no upstreams")
	}
}

func TestWriteDnsmasqConfigFor_emptyTargetDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	fakeResolv := writeTempFile(t, "nameserver 127.0.0.53\n")
	origPaths := resolvPaths
	origNmcli := nmcliDNSFunc
	resolvPaths = []string{fakeResolv}
	nmcliDNSFunc = func() []string { return nil }
	defer func() { resolvPaths = origPaths; nmcliDNSFunc = origNmcli }()

	if err := WriteDnsmasqConfigFor(dir, ""); err != nil {
		t.Fatalf("WriteDnsmasqConfigFor: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "lerd.conf"))
	assertContains(t, content, "address=/.test/127.0.0.1")
}

// --- lerdDNSInterfaces parsing ---

func TestLerdDNSInterfaces_multipleLinks(t *testing.T) {
	output := `Global
           Protocols: +LLMNR +mDNS
    resolv.conf mode: foreign

Link 2 (enp14s0)
    Current Scopes: DNS
Current DNS Server: 192.168.0.151
       DNS Servers: 192.168.0.151

Link 3 (wlan0)
    Current Scopes: none

Link 4 (virbr0)
    Current Scopes: DNS
Current DNS Server: 127.0.0.1:5300
       DNS Servers: 127.0.0.1:5300
        DNS Domain: ~test ~.

Link 6 (vnet1)
    Current Scopes: DNS
Current DNS Server: 127.0.0.1:5300
       DNS Servers: 127.0.0.1:5300
        DNS Domain: ~test ~.
`
	ifaces := parseLerdDNSInterfaces(output)
	want := []string{"virbr0", "vnet1"}
	assertSliceEqual(t, ifaces, want)
}

func TestLerdDNSInterfaces_none(t *testing.T) {
	output := `Link 2 (enp14s0)
Current DNS Server: 192.168.0.151
       DNS Servers: 192.168.0.151
`
	ifaces := parseLerdDNSInterfaces(output)
	if len(ifaces) != 0 {
		t.Errorf("expected empty, got %v", ifaces)
	}
}

// --- ResolverHint ---

func TestResolverHint_NetworkManager(t *testing.T) {
	origNM := isNetworkManagerActive
	origResolved := isSystemdResolvedActive
	defer func() { isNetworkManagerActive = origNM; isSystemdResolvedActive = origResolved }()

	isNetworkManagerActive = func() bool { return true }
	isSystemdResolvedActive = func() bool { return true }

	got := ResolverHint()
	if got != "sudo systemctl restart NetworkManager" {
		t.Errorf("expected NM hint, got %q", got)
	}
}

func TestResolverHint_SystemdResolvedOnly(t *testing.T) {
	origNM := isNetworkManagerActive
	origResolved := isSystemdResolvedActive
	defer func() { isNetworkManagerActive = origNM; isSystemdResolvedActive = origResolved }()

	isNetworkManagerActive = func() bool { return false }
	isSystemdResolvedActive = func() bool { return true }

	got := ResolverHint()
	if got != "sudo systemctl restart systemd-resolved" {
		t.Errorf("expected systemd-resolved hint, got %q", got)
	}
}

func TestResolverHint_NoResolver(t *testing.T) {
	origNM := isNetworkManagerActive
	origResolved := isSystemdResolvedActive
	defer func() { isNetworkManagerActive = origNM; isSystemdResolvedActive = origResolved }()

	isNetworkManagerActive = func() bool { return false }
	isSystemdResolvedActive = func() bool { return false }

	got := ResolverHint()
	if got != "restart your DNS resolver" {
		t.Errorf("expected generic hint, got %q", got)
	}
}

// --- helpers (Linux-only) ---

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
