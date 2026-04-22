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
