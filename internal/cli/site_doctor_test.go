package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/sitedoctor"
)

func TestResolveSiteDoctorTarget_DefaultsToCwd(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path, _, label, err := resolveSiteDoctorTarget("")
	if err != nil {
		t.Fatalf("cwd target: %v", err)
	}
	if path != cwd || label != cwd {
		t.Errorf("expected cwd %q, got path=%q label=%q", cwd, path, label)
	}
}

func TestResolveSiteDoctorTarget_UnknownDomain(t *testing.T) {
	if _, _, _, err := resolveSiteDoctorTarget("does-not-exist.invalid"); err == nil {
		t.Error("expected an error for an unknown domain")
	}
}

func TestDoctorGlyph(t *testing.T) {
	cases := map[string]string{
		sitedoctor.StatusOK:      "✓",
		sitedoctor.StatusWarn:    "⚠",
		sitedoctor.StatusFail:    "✗",
		sitedoctor.StatusUnknown: "?",
	}
	for status, glyph := range cases {
		if got := doctorGlyph(status); !strings.Contains(got, glyph) {
			t.Errorf("doctorGlyph(%q)=%q, want it to contain %q", status, got, glyph)
		}
	}
}
