package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrubHomePath_replacesHome(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	in := "log line referencing /home/testuser/.config/lerd/config.yaml here"
	out := scrubHomePath(in)
	if strings.Contains(out, "/home/testuser") {
		t.Fatalf("home path not scrubbed: %q", out)
	}
	if !strings.Contains(out, "$HOME/.config/lerd/config.yaml") {
		t.Fatalf("expected $HOME placeholder, got: %q", out)
	}
}

func TestScrubHomePath_emptyHome(t *testing.T) {
	t.Setenv("HOME", "")
	in := "no home set"
	if got := scrubHomePath(in); got != in {
		t.Fatalf("expected unchanged, got %q", got)
	}
}

func TestScrubHomePath_rootHome(t *testing.T) {
	t.Setenv("HOME", "/")
	in := "/etc/foo and / and /home/x"
	if got := scrubHomePath(in); got != in {
		t.Fatalf("expected unchanged when HOME=/, got %q", got)
	}
}

func TestWriteBugReportHeader_includesVersionAndOS(t *testing.T) {
	var buf bytes.Buffer
	writeBugReportHeader(&buf)
	out := buf.String()
	for _, want := range []string{"Lerd bug report", "lerd:", "OS:", "Generated:"} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q\n%s", want, out)
		}
	}
}

func TestWriteBugReport_createsFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "report.txt")
	got, err := writeBugReport(target, 5)
	if err != nil {
		t.Fatalf("writeBugReport: %v", err)
	}
	if got != target {
		t.Errorf("path mismatch: got %s want %s", got, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	for _, want := range []string{"Lerd bug report", "Doctor", "Config files", "Environment"} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestWriteBugReport_defaultPath(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	got, err := writeBugReport("", 5)
	if err != nil {
		t.Fatalf("writeBugReport: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(got), "lerd-bug-report-") {
		t.Errorf("default filename doesn't start with lerd-bug-report-: %s", got)
	}
	// EvalSymlinks both sides because macOS resolves /var → /private/var,
	// so t.TempDir() and os.Getwd()-after-chdir return different forms.
	gotDir, _ := filepath.EvalSymlinks(filepath.Dir(got))
	wantDir, _ := filepath.EvalSymlinks(dir)
	if gotDir != wantDir {
		t.Errorf("default file not in cwd: %s (cwd=%s)", got, dir)
	}
}

func TestEnvAllowlist_excludesSecrets(t *testing.T) {
	for _, key := range envAllowlist {
		switch strings.ToUpper(key) {
		case "AWS_SECRET_ACCESS_KEY", "GITHUB_TOKEN", "ANTHROPIC_API_KEY":
			t.Errorf("envAllowlist must not contain %q", key)
		}
		if strings.Contains(strings.ToUpper(key), "TOKEN") ||
			strings.Contains(strings.ToUpper(key), "SECRET") ||
			strings.Contains(strings.ToUpper(key), "PASSWORD") ||
			strings.Contains(strings.ToUpper(key), "KEY") {
			t.Errorf("envAllowlist contains suspicious key: %q", key)
		}
	}
}
