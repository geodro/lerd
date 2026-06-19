package agentenv

import (
	"reflect"
	"testing"
)

func TestPassthrough(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		want    []string
	}{
		{
			name:    "forwards CLAUDECODE",
			environ: []string{"PATH=/bin", "CLAUDECODE=1", "HOME=/root"},
			want:    []string{"CLAUDECODE=1"},
		},
		{
			name:    "forwards AI_AGENT value verbatim for pattern matching",
			environ: []string{"AI_AGENT=github-copilot-cli"},
			want:    []string{"AI_AGENT=github-copilot-cli"},
		},
		{
			name:    "forwards multiple agent vars, ignores unrelated",
			environ: []string{"CLAUDE_CODE=1", "FOO=bar", "CURSOR_AGENT=1"},
			want:    []string{"CLAUDE_CODE=1", "CURSOR_AGENT=1"},
		},
		{
			name:    "returns nil when none set",
			environ: []string{"PATH=/bin", "HOME=/root"},
			want:    nil,
		},
		{
			name:    "ignores prefix-collision keys",
			environ: []string{"CLAUDECODE_EXTRA=1", "AI_AGENTS=2"},
			want:    nil,
		},
		{
			name:    "redacts secret-bearing vars to a presence placeholder",
			environ: []string{"COPILOT_GITHUB_TOKEN=ghp_realsecret"},
			want:    []string{"COPILOT_GITHUB_TOKEN=1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Passthrough(tt.environ)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Passthrough(%v) = %v, want %v", tt.environ, got, tt.want)
			}
		})
	}
}

func TestMCPInject(t *testing.T) {
	tests := []struct {
		name    string
		environ []string
		want    []string
	}{
		{
			name:    "injects neutral marker when host has no agent var",
			environ: []string{"PATH=/bin", "HOME=/root"},
			want:    []string{"AI_AGENT=lerd-mcp"},
		},
		{
			name:    "forwards real host agent var instead of the marker",
			environ: []string{"CLAUDECODE=1"},
			want:    []string{"CLAUDECODE=1"},
		},
		{
			name:    "does not double up when AI_AGENT already set on host",
			environ: []string{"AI_AGENT=cursor"},
			want:    []string{"AI_AGENT=cursor"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MCPInject(tt.environ)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MCPInject(%v) = %v, want %v", tt.environ, got, tt.want)
			}
		})
	}
}
