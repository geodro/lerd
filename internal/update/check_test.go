package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
)

func TestIsPrerelease(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"1.19.1", false},
		{"1.20.0-beta.1", true},
		{"1.20.0-rc.1", true},
		{"1.20.0", false},
		{"2.0.0-alpha", true},
	}
	for _, tt := range tests {
		if got := IsPrerelease(tt.in); got != tt.want {
			t.Errorf("IsPrerelease(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestCachedUpdateCheck_skipsPrereleaseForStableUsers pins the defense:
// when the cached latest tag is a prerelease (e.g. /releases/latest
// momentarily redirected to a beta) and the current version is stable,
// no update notification is emitted.
func TestCachedUpdateCheck_skipsPrereleaseForStableUsers(t *testing.T) {
	withTempCache(t, "v1.20.0-beta.1")
	info, err := CachedUpdateCheck("1.19.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Errorf("expected nil (no update) for stable on cached prerelease, got %+v", info)
	}
}

// TestCachedUpdateCheck_betaUserSeesNewerBeta confirms beta users still get
// notified about newer prereleases, since the filter only applies when the
// caller is on a stable release.
func TestCachedUpdateCheck_betaUserSeesNewerBeta(t *testing.T) {
	withTempCache(t, "v1.20.0-beta.2")
	info, err := CachedUpdateCheck("1.20.0-beta.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected update info for beta-on-beta upgrade")
	}
	if info.LatestVersion != "v1.20.0-beta.2" {
		t.Errorf("got %q, want v1.20.0-beta.2", info.LatestVersion)
	}
}

// TestCachedUpdateCheck_stableUserSeesNewerStable confirms the happy path
// is untouched: stable→stable upgrades still surface.
func TestCachedUpdateCheck_stableUserSeesNewerStable(t *testing.T) {
	withTempCache(t, "v1.19.2")
	info, err := CachedUpdateCheck("1.19.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected update info for stable→stable upgrade")
	}
	if info.LatestVersion != "v1.19.2" {
		t.Errorf("got %q, want v1.19.2", info.LatestVersion)
	}
}

// withTempCache pre-seeds the on-disk update-check cache so CachedUpdateCheck
// returns the given tag without hitting the network. Sets XDG_DATA_HOME (the
// var DataDir() reads) so the writes land in the temp dir and never touch
// the user's real ~/.local/share/lerd/update-check.json.
func withTempCache(t *testing.T, tag string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	cacheDir := filepath.Dir(config.UpdateCheckFile())
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	state := updateCheckState{LatestVersion: tag, CheckedAt: time.Now()}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(config.UpdateCheckFile(), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
