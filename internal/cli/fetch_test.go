package cli

import "testing"

func TestSupportedPHPVersionsIncludesLegacyAndCurrent(t *testing.T) {
	want := map[string]bool{"7.4": false, "8.0": false, "8.1": false, "8.5": false}
	for _, v := range SupportedPHPVersions {
		if _, ok := want[v]; ok {
			want[v] = true
		}
	}
	for v, found := range want {
		if !found {
			t.Errorf("SupportedPHPVersions missing %q: %v", v, SupportedPHPVersions)
		}
	}
}

func TestSupportedPHPVersionsAreValid(t *testing.T) {
	for _, v := range SupportedPHPVersions {
		if err := validatePHPVersion(v); err != nil {
			t.Errorf("validatePHPVersion(%q) = %v, want nil", v, err)
		}
	}
}

func TestIsSupportedPHPVersion(t *testing.T) {
	for _, v := range SupportedPHPVersions {
		if !IsSupportedPHPVersion(v) {
			t.Errorf("IsSupportedPHPVersion(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "5.6", "8", "8.9", "latest", "8.4.1"} {
		if IsSupportedPHPVersion(v) {
			t.Errorf("IsSupportedPHPVersion(%q) = true, want false", v)
		}
	}
}
