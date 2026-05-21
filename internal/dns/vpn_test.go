package dns

import "testing"

func TestIsVPNIface(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"tun0", true},
		{"utun3", true},
		{"tap0", true},
		{"wg0", true},
		{"ipsec0", true},
		{"ppp0", true},
		{"cscotun0", true},
		{"eth0", false},
		{"enp1s0", false},
		{"wlan0", false},
		{"lo", false},
		{"podman0", false},
		{"docker0", false},
	}
	for _, tc := range cases {
		if got := isVPNIface(tc.name); got != tc.want {
			t.Errorf("isVPNIface(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// VPNActive walks real host interfaces; the smoke test just pins that it
// never panics and returns a usable bool regardless of host state.
func TestVPNActive_doesNotPanic(t *testing.T) {
	_ = VPNActive()
}
