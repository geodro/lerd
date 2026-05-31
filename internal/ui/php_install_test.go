package ui

import (
	"reflect"
	"testing"
)

func TestInstallablePHPVersions(t *testing.T) {
	supported := []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"}

	tests := []struct {
		name      string
		installed []string
		want      []string
	}{
		{
			name:      "none installed returns all in order",
			installed: nil,
			want:      []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"},
		},
		{
			name:      "filters installed and preserves order",
			installed: []string{"8.3", "7.4"},
			want:      []string{"8.0", "8.1", "8.2", "8.4", "8.5"},
		},
		{
			name:      "all installed returns empty non-nil slice",
			installed: supported,
			want:      []string{},
		},
		{
			name:      "unknown installed version is ignored",
			installed: []string{"5.6"},
			want:      []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := installablePHPVersions(supported, tc.installed)
			if got == nil {
				t.Fatal("expected non-nil slice")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("installablePHPVersions(%v) = %v, want %v", tc.installed, got, tc.want)
			}
		})
	}
}
