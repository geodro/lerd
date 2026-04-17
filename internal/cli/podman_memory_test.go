package cli

import "testing"

func TestRecommendedVMMemoryMiB(t *testing.T) {
	cases := []struct {
		name string
		host int
		want int
	}{
		{"detection failed", 0, 4096},
		{"negative invalid", -1, 4096},
		{"low-end 4GB", 4, 3072},
		{"8GB MacBook", 8, 3072},
		{"9GB edge", 9, 4096},
		{"16GB laptop", 16, 4096},
		{"31GB upper mid", 31, 4096},
		{"32GB workstation", 32, 6144},
		{"64GB power user", 64, 6144},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := recommendedVMMemoryMiB(c.host); got != c.want {
				t.Errorf("recommendedVMMemoryMiB(%d) = %d, want %d", c.host, got, c.want)
			}
		})
	}
}
