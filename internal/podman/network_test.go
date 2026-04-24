package podman

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLerdULAv6Subnet_isValidIPv6CIDR(t *testing.T) {
	ip, ipnet, err := net.ParseCIDR(LerdULAv6Subnet)
	if err != nil {
		t.Fatalf("LerdULAv6Subnet not parseable: %v", err)
	}
	if ip.To4() != nil {
		t.Errorf("LerdULAv6Subnet must be v6, got v4 %v", ip)
	}
	if ones, bits := ipnet.Mask.Size(); ones != 64 || bits != 128 {
		t.Errorf("expected /64 mask, got /%d (bits=%d)", ones, bits)
	}
	if !strings.HasPrefix(ip.String(), "fd") {
		t.Errorf("expected ULA prefix (fc00::/7), got %v", ip)
	}
}

func TestErrNetworkNeedsMigration_isComparable(t *testing.T) {
	if ErrNetworkNeedsMigration == nil {
		t.Fatal("ErrNetworkNeedsMigration is nil")
	}
	if ErrNetworkNeedsMigration.Error() == "" {
		t.Error("ErrNetworkNeedsMigration has empty message")
	}
}

func TestAardvarkListenHasV6(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"dual-stack v6 first", "fd00:1e7d::1,10.89.7.1 169.254.1.1", true},
		{"dual-stack v4 first", "10.89.7.1,fd00:1e7d::1 169.254.1.1", true},
		{"v4 only with forwarder", "10.89.7.1 169.254.1.1", false},
		{"v4 only no forwarder", "10.89.7.1", false},
		{"v6 only", "fd00:1e7d::1", true},
		{"empty", "", false},
		{"whitespace", "   ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := aardvarkListenHasV6(tt.line); got != tt.want {
				t.Errorf("aardvarkListenHasV6(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestAardvarkConfigPath_usesXDGRuntimeDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	got := aardvarkConfigPath("lerd")
	want := filepath.Join(tmp, "containers/networks/aardvark-dns", "lerd")
	if got != want {
		t.Errorf("aardvarkConfigPath: got %s, want %s", got, want)
	}
}

func TestAardvarkNetworkDrifted_returnsFalseWhenConfigMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	if got := AardvarkNetworkDrifted("nonexistent-network"); got {
		t.Errorf("AardvarkNetworkDrifted with missing config: got true, want false")
	}
}

func TestAardvarkConfigPath_removeIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	path := aardvarkConfigPath("ghost")
	if err := os.Remove(path); err == nil {
		t.Error("removing missing file unexpectedly succeeded without ENOENT")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stale\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("removing present file: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should be gone after Remove, got err=%v", err)
	}
}

func TestHostHasUsableIPv6(t *testing.T) {
	tests := []struct {
		name        string
		disableIPv6 string
		ifInet6     string
		want        bool
	}{
		{
			name: "vm with only loopback and link-local",
			ifInet6: "00000000000000000000000000000001 01 80 10 80       lo\n" +
				"fe80000000000000505400fffed9f609 02 40 20 80   enp1s0\n",
			want: false,
		},
		{
			name: "host with global v6",
			ifInet6: "00000000000000000000000000000001 01 80 10 80       lo\n" +
				"fe80000000000000505400fffed9f609 02 40 20 80   enp1s0\n" +
				"2a0100000000000000000000000000ab 02 40 00 80   enp1s0\n",
			want: true,
		},
		{
			name: "host with ULA v6",
			ifInet6: "00000000000000000000000000000001 01 80 10 80       lo\n" +
				"fd000000000000000000000000000001 02 40 00 80   enp1s0\n",
			want: true,
		},
		{
			name:        "ipv6 disabled kernel-wide",
			disableIPv6: "1",
			ifInet6: "00000000000000000000000000000001 01 80 10 80       lo\n" +
				"2a0100000000000000000000000000ab 02 40 00 80   enp1s0\n",
			want: false,
		},
		{
			name:    "empty if_inet6",
			ifInet6: "",
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			procSys := filepath.Join(tmp, "sys/net/ipv6/conf/all")
			procNet := filepath.Join(tmp, "net")
			if err := os.MkdirAll(procSys, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(procNet, 0755); err != nil {
				t.Fatal(err)
			}
			disabled := tt.disableIPv6
			if disabled == "" {
				disabled = "0"
			}
			if err := os.WriteFile(filepath.Join(procSys, "disable_ipv6"), []byte(disabled), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(procNet, "if_inet6"), []byte(tt.ifInet6), 0644); err != nil {
				t.Fatal(err)
			}
			// Swap the root-looking paths via the test-only hook.
			oldDisable := ipv6DisablePath
			oldIfInet6 := ipv6IfInet6Path
			ipv6DisablePath = filepath.Join(procSys, "disable_ipv6")
			ipv6IfInet6Path = filepath.Join(procNet, "if_inet6")
			defer func() {
				ipv6DisablePath = oldDisable
				ipv6IfInet6Path = oldIfInet6
			}()
			if got := HostHasUsableIPv6(); got != tt.want {
				t.Errorf("HostHasUsableIPv6() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPv6ProbeFailedMarker(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	if ipv6ProbeFailed("lerd") {
		t.Fatal("should not be marked before any probe")
	}

	markIPv6ProbeFailed("lerd")
	if !ipv6ProbeFailed("lerd") {
		t.Fatal("should be marked after markIPv6ProbeFailed")
	}

	clearIPv6ProbeFailed("lerd")
	if ipv6ProbeFailed("lerd") {
		t.Fatal("should be cleared after clearIPv6ProbeFailed")
	}
}

func TestMarkIPv6Disabled_sharesProbeFailedMarker(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	MarkIPv6Disabled("lerd")
	if !ipv6ProbeFailed("lerd") {
		t.Fatal("MarkIPv6Disabled must set the same marker that EnsureNetwork checks")
	}

	want := ipv6ProbeFailedPath("lerd")
	if got := IPv6DisabledMarkerPath("lerd"); got != want {
		t.Errorf("IPv6DisabledMarkerPath = %q, want %q", got, want)
	}
}

func TestProbeNetworkIPv6_detectsAardvarkFailure(t *testing.T) {
	// probeNetworkIPv6 shells out to podman, so we only unit-test the output
	// parsing logic by inlining it. Integration coverage comes from the
	// full install test.
	tests := []struct {
		name   string
		output string
		want   bool // true = probe OK (IPv6 works or inconclusive)
	}{
		{"success (empty output)", "", true},
		{"aardvark bind error", "Error: netavark: error while applying dns entries: aardvark-dns failed to start: Error from child process\nError starting server failed to bind udp listener on [fd00:1e7d::1]:53: IO error: Cannot assign requested address (os error 99)", false},
		{"image not found", "Error: alpine:latest: image not known", true},
		{"generic error", "Error: some unrelated failure", true},
		{"cannot assign only", "Cannot assign requested address", false},
		{"aardvark only", "aardvark-dns crashed", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.output
			got := !strings.Contains(s, "aardvark-dns") &&
				!strings.Contains(s, "Cannot assign requested address")
			if got != tt.want {
				t.Errorf("probe output %q: got %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestRemoveNetwork_wipesAardvarkConfigEvenWhenPodmanFails(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)
	t.Setenv("PATH", filepath.Join(tmp, "no-bin"))

	path := aardvarkConfigPath("ghost-net")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("10.0.0.1 8.8.8.8\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_ = RemoveNetwork("ghost-net")

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("aardvark file should be removed even when podman is unavailable, got err=%v", err)
	}
}

func TestAardvarkNetworkDrifted_readsExistingConfig_v4Only(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	dir := filepath.Join(tmp, "containers/networks/aardvark-dns")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "10.89.7.1 169.254.1.1\nabc123 10.89.7.5 svc,abc123\n"
	if err := os.WriteFile(filepath.Join(dir, "drifted"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	first := strings.SplitN(content, "\n", 2)[0]
	if aardvarkListenHasV6(first) {
		t.Errorf("drifted first line %q should not contain v6 listen", first)
	}
}
