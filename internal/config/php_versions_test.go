package config

import (
	"strings"
	"testing"
)

func TestIsSupportedPHPVersion(t *testing.T) {
	for _, v := range []string{"7.4", "8.0", "8.5"} {
		if !IsSupportedPHPVersion(v) {
			t.Errorf("IsSupportedPHPVersion(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"5.6", "9.0", ""} {
		if IsSupportedPHPVersion(v) {
			t.Errorf("IsSupportedPHPVersion(%q) = true, want false", v)
		}
	}
}

func TestFrankenPHPVersions(t *testing.T) {
	got := FrankenPHPVersions()
	want := []string{"8.2", "8.3", "8.4", "8.5"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("FrankenPHPVersions() = %v, want %v", got, want)
	}
}

func TestIsFrankenPHPVersion(t *testing.T) {
	cases := map[string]bool{
		"7.4": false, "8.0": false, "8.1": false,
		"8.2": true, "8.4": true, "8.5": true,
		"9.0": false, "": false,
	}
	for v, want := range cases {
		if got := IsFrankenPHPVersion(v); got != want {
			t.Errorf("IsFrankenPHPVersion(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestLatestFrankenPHPVersion(t *testing.T) {
	got := LatestFrankenPHPVersion()
	want := FrankenPHPVersions()[len(FrankenPHPVersions())-1]
	if got != want {
		t.Errorf("LatestFrankenPHPVersion() = %q, want %q", got, want)
	}
	if got != "8.5" {
		t.Errorf("LatestFrankenPHPVersion() = %q, want 8.5", got)
	}
}

func TestPHPVersionAtLeast(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"8.2", "8.2", true},
		{"8.5", "8.2", true},
		{"8.1", "8.2", false},
		{"7.4", "8.2", false},
		{"8.10", "8.9", true},
		{"9.0", "8.5", true},
	}
	for _, c := range cases {
		if got := phpVersionAtLeast(c.a, c.b); got != c.want {
			t.Errorf("phpVersionAtLeast(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
