package registry

import (
	"testing"
	"time"
)

func TestParseImage(t *testing.T) {
	cases := []struct {
		in           string
		wantRegistry string
		wantRepo     string
		wantTag      string
	}{
		{"docker.io/library/mysql:5.7", "docker.io", "library/mysql", "5.7"},
		{"docker.io/postgis/postgis:16-3.5-alpine", "docker.io", "postgis/postgis", "16-3.5-alpine"},
		{"docker.io/library/redis:7-alpine", "docker.io", "library/redis", "7-alpine"},
		{"ghcr.io/owner/repo:v1.2.3", "ghcr.io", "owner/repo", "v1.2.3"},
		// Implicit Docker Hub registry.
		{"library/mysql:5.7", "docker.io", "library/mysql", "5.7"},
		{"mysql:5.7", "docker.io", "library/mysql", "5.7"},
		// No tag = latest.
		{"redis", "docker.io", "library/redis", "latest"},
	}
	for _, tc := range cases {
		got, err := ParseImage(tc.in)
		if err != nil {
			t.Errorf("ParseImage(%q) error: %v", tc.in, err)
			continue
		}
		if got.Registry != tc.wantRegistry || got.Repo != tc.wantRepo || got.Tag != tc.wantTag {
			t.Errorf("ParseImage(%q) = {%q, %q, %q}, want {%q, %q, %q}",
				tc.in, got.Registry, got.Repo, got.Tag,
				tc.wantRegistry, tc.wantRepo, tc.wantTag)
		}
	}
}

func TestParseTag(t *testing.T) {
	cases := []struct {
		in          string
		wantBase    string
		wantVariant string
		wantNumeric []int
	}{
		{"5.7", "5.7", "", []int{5, 7}},
		{"8.0.34", "8.0.34", "", []int{8, 0, 34}},
		{"16-3.5-alpine", "16", "-3.5-alpine", []int{16}},
		{"7-alpine", "7", "-alpine", []int{7}},
		{"v1.7", "1.7", "", []int{1, 7}},
		{"latest", "latest", "", nil},
		{"main", "main", "", nil},
	}
	for _, tc := range cases {
		got := parseTag(tc.in)
		if got.Base != tc.wantBase || got.Variant != tc.wantVariant {
			t.Errorf("parseTag(%q) = {%q, %q}, want {%q, %q}",
				tc.in, got.Base, got.Variant, tc.wantBase, tc.wantVariant)
		}
		if !sameInts(got.Numeric, tc.wantNumeric) {
			t.Errorf("parseTag(%q).Numeric = %v, want %v", tc.in, got.Numeric, tc.wantNumeric)
		}
	}
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPickNewer_MinorStrategy_Mysql(t *testing.T) {
	// Current is mysql 8.0; minor strategy accepts any 8.x but rejects 9.x.
	current := parseTag("8.0")
	candidates := []TagInfo{
		{Name: "8.0", Pushed: time.Unix(100, 0)},
		{Name: "8.0.34", Pushed: time.Unix(200, 0)},
		{Name: "8.4.3", Pushed: time.Unix(300, 0)},
		{Name: "9.0", Pushed: time.Unix(400, 0)}, // major bump — must skip
		{Name: "9.1.0", Pushed: time.Unix(500, 0)},
		{Name: "latest", Pushed: time.Unix(999, 0)},
	}
	picked := pickNewer(current, candidates, StrategyMinor)
	if picked == nil {
		t.Fatal("expected a candidate to win, got nil")
	}
	if picked.Name != "8.4.3" {
		t.Errorf("minor strategy picked %q, want 8.4.3 (newest 8.x)", picked.Name)
	}
}

func TestPickNewer_MinorStrategy_Postgres_VariantMustMatch(t *testing.T) {
	// Current is postgis 16-3.5-alpine. minor accepts 16.x WITH the same
	// "-3.5-alpine" variant, rejecting bare 16 or alpine-less variants.
	current := parseTag("16-3.5-alpine")
	candidates := []TagInfo{
		{Name: "16", Pushed: time.Unix(100, 0)},                  // variant mismatch
		{Name: "16.1-3.5-alpine", Pushed: time.Unix(150, 0)},     // eligible newer minor
		{Name: "16.4-3.5-alpine", Pushed: time.Unix(250, 0)},     // eligible newer minor
		{Name: "16.4-3.5-alpine3.20", Pushed: time.Unix(300, 0)}, // variant mismatch
		{Name: "17-3.5-alpine", Pushed: time.Unix(400, 0)},       // major bump — skip
	}
	picked := pickNewer(current, candidates, StrategyMinor)
	if picked == nil {
		t.Fatal("expected an eligible candidate")
	}
	if picked.Name != "16.4-3.5-alpine" {
		t.Errorf("minor+variant picked %q, want 16.4-3.5-alpine (highest within major+variant)", picked.Name)
	}
}

func TestPickNewer_RollingStrategy_DigestChange(t *testing.T) {
	// Rolling tag (e.g. mailpit:latest) — pickNewer must look at the same
	// tag name and only return it when the digest differs from current.
	current := parseTag("latest")
	candidates := []TagInfo{
		{Name: "latest", Digest: "sha256:abc", Pushed: time.Unix(100, 0)},
	}
	picked := pickNewer(current, candidates, StrategyRolling)
	if picked == nil {
		t.Fatal("rolling: expected the same-name candidate to be returned")
	}
}

func TestPickNewer_NoneStrategy_AlwaysSkips(t *testing.T) {
	current := parseTag("8.0")
	candidates := []TagInfo{
		{Name: "9.0", Pushed: time.Unix(400, 0)},
	}
	if picked := pickNewer(current, candidates, StrategyNone); picked != nil {
		t.Errorf("strategy=none must never recommend an update, got %+v", picked)
	}
}

func TestPickNewer_PatchStrategy(t *testing.T) {
	// Current 8.0.34 — patch accepts 8.0.x but rejects 8.1.x.
	current := parseTag("8.0.34")
	candidates := []TagInfo{
		{Name: "8.0.35", Pushed: time.Unix(100, 0)},
		{Name: "8.0.36", Pushed: time.Unix(200, 0)},
		{Name: "8.1.0", Pushed: time.Unix(300, 0)},
	}
	picked := pickNewer(current, candidates, StrategyPatch)
	if picked == nil || picked.Name != "8.0.36" {
		t.Errorf("patch picked %v, want 8.0.36", picked)
	}
}

func TestPickNewer_NotNewerThanCurrent(t *testing.T) {
	// Same tag, no digest change, no new candidates → no update.
	current := parseTag("8.0.34")
	candidates := []TagInfo{
		{Name: "8.0.34", Pushed: time.Unix(100, 0)},
		{Name: "8.0.30", Pushed: time.Unix(50, 0)},
	}
	if picked := pickNewer(current, candidates, StrategyMinor); picked != nil {
		t.Errorf("expected no recommendation when current is newest, got %+v", picked)
	}
}
