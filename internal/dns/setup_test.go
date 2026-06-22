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

func TestParseNmcliOutput_skipsZonedLinkLocal(t *testing.T) {
	input := "fe80::46d4:53ff:fe3f:a9a7%18|8.8.8.8\nfe80::1%eth0\n"
	got := parseNmcliLines(input)
	want := []string{"8.8.8.8"}
	assertSliceEqual(t, got, want)
}

func TestParseNmcliOutput_empty(t *testing.T) {
	got := parseNmcliLines("")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// --- upstreamOrPasta ---

func TestUpstreamOrPasta_usesUpstreamsWhenPresent(t *testing.T) {
	fakeResolv := writeTempFile(t, "nameserver 8.8.8.8\n")
	origPaths := resolvPaths
	resolvPaths = []string{fakeResolv}
	defer func() { resolvPaths = origPaths }()

	got := upstreamOrPasta()
	assertSliceEqual(t, got, []string{"8.8.8.8"})
}

func TestUpstreamOrPasta_fallsBackToPastaForwarder(t *testing.T) {
	emptyResolv := writeTempFile(t, "# empty\n")
	origPaths := resolvPaths
	origNmcli := nmcliDNSFunc
	resolvPaths = []string{emptyResolv}
	nmcliDNSFunc = func() []string { return nil }
	defer func() { resolvPaths = origPaths; nmcliDNSFunc = origNmcli }()

	got := upstreamOrPasta()
	assertSliceEqual(t, got, []string{pastaDefaultForwarder})
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

func TestWriteDnsmasqConfig_noUpstreamsFallsBackToPasta(t *testing.T) {
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
	assertContains(t, content, "address=/.test/::1")
	assertContains(t, content, "no-resolv")
	assertContains(t, content, "server="+pastaDefaultForwarder)
	if strings.Contains(content, "listen-address") {
		t.Errorf("dnsmasq must not restrict listen-address (rootlessport forwards via container netif, not loopback), got:\n%s", content)
	}
}

func TestWriteDnsmasqConfig_pinnedUpstreamOverridesResolv(t *testing.T) {
	writeGlobalConfig(t, "dns:\n  upstream:\n    - 192.168.100.129\n")
	dir := t.TempDir()
	fakeResolv := writeTempFile(t, "nameserver 9.9.9.9\nnameserver 8.8.8.8\n")
	origPaths := resolvPaths
	resolvPaths = []string{fakeResolv}
	defer func() { resolvPaths = origPaths }()

	if err := WriteDnsmasqConfig(dir); err != nil {
		t.Fatalf("WriteDnsmasqConfig: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "lerd.conf"))
	assertContains(t, content, "server=192.168.100.129")
	if strings.Contains(content, "server=9.9.9.9") || strings.Contains(content, "server=8.8.8.8") {
		t.Errorf("pinned upstream must replace detected resolv.conf servers, got:\n%s", content)
	}
}

// --- NM dispatcher script ---

func TestNMDispatcherScript_runsAsRealUser(t *testing.T) {
	assertContains(t, nmDispatcherScript, "runuser -u")
}

func TestNMDispatcherScript_prefersPinnedUpstream(t *testing.T) {
	assertContains(t, nmDispatcherScript, "upstream:")
	assertContains(t, nmDispatcherScript, "dns_servers=\"$LERD_DNS\"")
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
	assertContains(t, content, "no-resolv")
	assertContains(t, content, "server="+pastaDefaultForwarder)
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

// --- v6 dnsmasq output ---

func TestWriteDnsmasqConfig_emitsV6Listen(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	fakeResolv := writeTempFile(t, "nameserver 8.8.8.8\n")
	origPaths := resolvPaths
	resolvPaths = []string{fakeResolv}
	defer func() { resolvPaths = origPaths }()

	if err := WriteDnsmasqConfig(dir); err != nil {
		t.Fatalf("WriteDnsmasqConfig: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "lerd.conf"))
	assertContains(t, content, "address=/.test/127.0.0.1")
	assertContains(t, content, "address=/.test/::1")
}

func TestWriteDnsmasqConfigDual_skipsV6WhenEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	fakeResolv := writeTempFile(t, "nameserver 8.8.8.8\n")
	origPaths := resolvPaths
	resolvPaths = []string{fakeResolv}
	defer func() { resolvPaths = origPaths }()

	if err := WriteDnsmasqConfigDual(dir, "10.0.0.5", ""); err != nil {
		t.Fatalf("WriteDnsmasqConfigDual: %v", err)
	}
	content := readFile(t, filepath.Join(dir, "lerd.conf"))
	assertContains(t, content, "address=/.test/10.0.0.5")
	if strings.Contains(content, "address=/.test/::") {
		t.Errorf("expected no v6 address record when v6Target empty, got:\n%s", content)
	}
}

func TestDeriveV6Target(t *testing.T) {
	cases := []struct {
		v4   string
		want string
	}{
		{"", "::1"},
		{"127.0.0.1", "::1"},
	}
	for _, c := range cases {
		if got := deriveV6Target(c.v4); got != c.want {
			t.Errorf("deriveV6Target(%q) = %q, want %q", c.v4, got, c.want)
		}
	}
	// LAN target derives to either a global v6 (if host has one) or ::1
	// fallback. Both are acceptable; assert it never returns empty.
	if got := deriveV6Target("10.0.0.5"); got == "" {
		t.Error("deriveV6Target(LAN) returned empty, expected global v6 or ::1")
	}
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
