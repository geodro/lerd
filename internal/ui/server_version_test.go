package ui

import (
	"strings"
	"testing"

	lerdUpdate "github.com/geodro/lerd/internal/update"
)

// TestBuildVersionResponse_StripsLeadingV pins the fix for "Lerd vv1.19.2
// is available" — the GitHub tag is e.g. "v1.19.2" but the Svelte banner
// template already prepends "v", so the wire data must be bare.
func TestBuildVersionResponse_StripsLeadingV(t *testing.T) {
	resp := buildVersionResponse("1.19.1", &lerdUpdate.UpdateInfo{LatestVersion: "v1.19.2"})
	if resp.Latest != "1.19.2" {
		t.Errorf("Latest = %q, want %q (no leading v)", resp.Latest, "1.19.2")
	}
	if !resp.HasUpdate {
		t.Errorf("HasUpdate should be true when info is non-nil")
	}
}

// TestBuildVersionResponse_NoUpdateLeavesLatestEmpty keeps the banner hidden
// when no update is available — no Latest, no HasUpdate.
func TestBuildVersionResponse_NoUpdateLeavesLatestEmpty(t *testing.T) {
	resp := buildVersionResponse("1.19.1", nil)
	if resp.Latest != "" || resp.HasUpdate {
		t.Errorf("expected zero-update response, got %+v", resp)
	}
	if resp.Current != "1.19.1" {
		t.Errorf("Current should be passed through, got %q", resp.Current)
	}
}

// TestBuildVersionResponse_HandlesPrereleaseTag covers the beta channel
// where the tag is e.g. "v1.20.0-beta.1" — strip-v still applies.
func TestBuildVersionResponse_HandlesPrereleaseTag(t *testing.T) {
	resp := buildVersionResponse("1.20.0-beta.1", &lerdUpdate.UpdateInfo{LatestVersion: "v1.20.0-beta.2"})
	if resp.Latest != "1.20.0-beta.2" {
		t.Errorf("prerelease Latest mishandled, got %q", resp.Latest)
	}
}

// TestBuildUpdateScript_UsesAbsolutePath pins the fix for "lerd: command
// not found" when the dashboard's "Open terminal & update" button spawned
// a terminal whose non-login shell didn't have ~/.local/bin on PATH.
// The script must reference the resolved executable, not the bare name.
func TestBuildUpdateScript_UsesAbsolutePath(t *testing.T) {
	got := buildUpdateScript("/home/alice/.local/bin/lerd")
	if !strings.Contains(got, "/home/alice/.local/bin/lerd") {
		t.Errorf("script should reference absolute path, got %q", got)
	}
	if strings.HasPrefix(got, "lerd ") {
		t.Errorf("script should not start with bare 'lerd', got %q", got)
	}
	if !strings.Contains(got, " update;") {
		t.Errorf("script should run `update` subcommand, got %q", got)
	}
	if !strings.Contains(got, "Press Enter to close") {
		t.Errorf("script should keep the wait-for-input tail, got %q", got)
	}
}

// TestBuildUpdateScript_QuotesPathWithSpaces protects the shell substitution
// when the binary lives under a path with spaces (a Mac install in
// "/Users/J D/.local/bin/lerd", say). shQuote should single-quote it.
func TestBuildUpdateScript_QuotesPathWithSpaces(t *testing.T) {
	got := buildUpdateScript("/Users/J D/.local/bin/lerd")
	if !strings.Contains(got, `'/Users/J D/.local/bin/lerd'`) {
		t.Errorf("path with spaces should be single-quoted, got %q", got)
	}
}
