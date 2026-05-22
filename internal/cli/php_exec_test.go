package cli

import (
	"reflect"
	"testing"
)

func TestSpxPassthroughEnv(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		want    []string
	}{
		{
			name:    "no SPX vars yields nothing",
			environ: []string{"HOME=/home/x", "PATH=/bin"},
			want:    nil,
		},
		{
			name:    "enabled with no report defaults to full",
			environ: []string{"PATH=/bin", "SPX_ENABLED=1"},
			want:    []string{"SPX_ENABLED=1", "SPX_REPORT=full"},
		},
		{
			name:    "explicit report is left untouched",
			environ: []string{"SPX_ENABLED=1", "SPX_REPORT=fp"},
			want:    []string{"SPX_ENABLED=1", "SPX_REPORT=fp"},
		},
		{
			name:    "disabled SPX never gets a default report",
			environ: []string{"SPX_ENABLED=0"},
			want:    []string{"SPX_ENABLED=0"},
		},
		{
			name:    "other SPX vars are forwarded as-is",
			environ: []string{"SPX_SAMPLING_PERIOD=100", "SPX_ENABLED=1"},
			want:    []string{"SPX_SAMPLING_PERIOD=100", "SPX_ENABLED=1", "SPX_REPORT=full"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := spxPassthroughEnv(tt.environ)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("spxPassthroughEnv(%v) = %v, want %v", tt.environ, got, tt.want)
			}
		})
	}
}
