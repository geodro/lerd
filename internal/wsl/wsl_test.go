package wsl

import (
	"strings"
	"testing"
)

func TestIsWSL_EnvSignal(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu-24.04")
	if !IsWSL() {
		t.Error("WSL_DISTRO_NAME set should report WSL")
	}
	t.Setenv("WSL_DISTRO_NAME", "")
	// With the env cleared, the result depends on the host kernel; just assert
	// it doesn't panic and returns a bool.
	_ = IsWSL()
}

func TestEnsureSectionLine_AddsMissingSection(t *testing.T) {
	got, changed := EnsureSectionLine("", "boot", "systemd", "systemd=true")
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(got, "[boot]") || !strings.Contains(got, "systemd=true") {
		t.Errorf("missing section/line: %q", got)
	}
}

func TestEnsureSectionLine_Idempotent(t *testing.T) {
	start := "[boot]\nsystemd=true\n"
	got, changed := EnsureSectionLine(start, "boot", "systemd", "systemd=true")
	if changed {
		t.Errorf("already-set line should not change: %q", got)
	}
	if got != start {
		t.Errorf("content mutated: %q", got)
	}
}

func TestEnsureSectionLine_RewritesWrongValue(t *testing.T) {
	got, changed := EnsureSectionLine("[boot]\nsystemd=false\n", "boot", "systemd", "systemd=true")
	if !changed {
		t.Fatal("expected rewrite")
	}
	if strings.Contains(got, "systemd=false") || !strings.Contains(got, "systemd=true") {
		t.Errorf("did not rewrite: %q", got)
	}
}

func TestEnsureSectionLine_InsertsIntoExistingSection(t *testing.T) {
	// A [wsl2] section that already has one key; adding another must land in it,
	// not create a duplicate section.
	start := "[wsl2]\nmemory=8GB\n"
	got, changed := EnsureSectionLine(start, "wsl2", "networkingMode", "networkingMode=mirrored")
	if !changed {
		t.Fatal("expected change")
	}
	if strings.Count(got, "[wsl2]") != 1 {
		t.Errorf("duplicated section: %q", got)
	}
	if !strings.Contains(got, "networkingMode=mirrored") || !strings.Contains(got, "memory=8GB") {
		t.Errorf("lost a key: %q", got)
	}
}

func TestEnsureSectionLine_KeyBeforeNextSectionPreserved(t *testing.T) {
	// Inserting into a section that's followed by another section must keep the
	// later section intact.
	start := "[wsl2]\nmemory=8GB\n\n[experimental]\nfoo=bar\n"
	got, _ := EnsureSectionLine(start, "wsl2", "networkingMode", "networkingMode=mirrored")
	if !strings.Contains(got, "[experimental]") || !strings.Contains(got, "foo=bar") {
		t.Errorf("clobbered the following section: %q", got)
	}
	if !strings.Contains(got, "networkingMode=mirrored") {
		t.Errorf("did not insert: %q", got)
	}
}

func TestWSLConfigLines_AppliedAllAndIdempotent(t *testing.T) {
	content := ""
	for _, kv := range WSLConfigLines {
		content, _ = EnsureSectionLine(content, "wsl2", kv.Key, kv.Line)
	}
	for _, kv := range WSLConfigLines {
		if !strings.Contains(content, kv.Line) {
			t.Errorf("missing %q", kv.Line)
		}
	}
	// localhostForwarding / pageReporting must NOT be recommended (they warn).
	if strings.Contains(content, "localhostForwarding") || strings.Contains(content, "pageReporting") {
		t.Errorf("recommended a warning-producing key: %q", content)
	}
	// Re-applying changes nothing.
	for _, kv := range WSLConfigLines {
		if _, changed := EnsureSectionLine(content, "wsl2", kv.Key, kv.Line); changed {
			t.Errorf("second pass changed %q", kv.Key)
		}
	}
}

func TestHasEventsLoggerJournald(t *testing.T) {
	if !HasEventsLoggerJournald("[engine]\nevents_logger = \"journald\"\n") {
		t.Error("quoted journald should match")
	}
	if !HasEventsLoggerJournald("[engine]\nevents_logger=journald\n") {
		t.Error("unquoted journald should match")
	}
	if HasEventsLoggerJournald("[engine]\nevents_logger = \"file\"\n") {
		t.Error("file must not match")
	}
}
