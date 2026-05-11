package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── removeMarkedBlock ────────────────────────────────────────────────────────

const testMarker = "# Added by Lerd installer"

func TestRemoveMarkedBlock_removesMarkerAndNextLine(t *testing.T) {
	tmp := t.TempDir()
	rc := filepath.Join(tmp, ".bashrc")

	content := "existing line\n" +
		testMarker + "\n" +
		`export PATH="/home/user/.local/share/lerd/bin:$PATH"` + "\n" +
		"another line\n"

	os.WriteFile(rc, []byte(content), 0644)
	removeMarkedBlock(rc, testMarker, 1)

	got, _ := os.ReadFile(rc)
	if strings.Contains(string(got), testMarker) {
		t.Error("marker line should have been removed")
	}
	if strings.Contains(string(got), "lerd/bin") {
		t.Error("PATH export line should have been removed")
	}
	if !strings.Contains(string(got), "existing line") {
		t.Error("unrelated lines should be preserved")
	}
	if !strings.Contains(string(got), "another line") {
		t.Error("lines after the block should be preserved")
	}
}

func TestRemoveMarkedBlock_noMarker_noChange(t *testing.T) {
	tmp := t.TempDir()
	rc := filepath.Join(tmp, ".bashrc")

	content := "line one\nline two\n"
	os.WriteFile(rc, []byte(content), 0644)
	removeMarkedBlock(rc, testMarker, 1)

	got, _ := os.ReadFile(rc)
	if string(got) != content {
		t.Errorf("file should be unchanged, got:\n%s", got)
	}
}

func TestRemoveMarkedBlock_missingFile_noError(t *testing.T) {
	// Must not panic or return an error — the function is best-effort.
	removeMarkedBlock("/tmp/lerd-test-nonexistent-file-xyz", testMarker, 1)
}

func TestRemoveMarkedBlock_markerAtEndOfFile(t *testing.T) {
	tmp := t.TempDir()
	rc := filepath.Join(tmp, ".zshrc")

	content := "source ~/.profile\n" + testMarker + "\n"
	os.WriteFile(rc, []byte(content), 0644)
	removeMarkedBlock(rc, testMarker, 1)

	got, _ := os.ReadFile(rc)
	if strings.Contains(string(got), testMarker) {
		t.Error("marker should have been removed")
	}
	if !strings.Contains(string(got), "source ~/.profile") {
		t.Error("preceding lines should be preserved")
	}
}

func TestRemoveMarkedBlock_onlyMarker(t *testing.T) {
	tmp := t.TempDir()
	rc := filepath.Join(tmp, ".bashrc")

	os.WriteFile(rc, []byte(testMarker+"\n"), 0644)
	removeMarkedBlock(rc, testMarker, 1)

	got, _ := os.ReadFile(rc)
	if strings.Contains(string(got), testMarker) {
		t.Error("marker should have been removed from single-line file")
	}
}

// ── readYes ──────────────────────────────────────────────────────────────────

func TestReadYes(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"N\n", false},
		{"no\n", false},
		{"\n", false},
		{"maybe\n", false},
	}

	for _, c := range cases {
		// Redirect stdin to a pipe containing the test input.
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		w.WriteString(c.input)
		w.Close()

		oldStdin := os.Stdin
		os.Stdin = r
		got := readYes()
		os.Stdin = oldStdin
		r.Close()

		if got != c.want {
			t.Errorf("readYes(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

// ── removeShellEntry ─────────────────────────────────────────────────────────

func TestRemoveShellEntry_bashrc(t *testing.T) {
	tmp := t.TempDir()

	// Simulate a home directory with a .bashrc containing the Lerd PATH block.
	bashrc := filepath.Join(tmp, ".bashrc")
	os.WriteFile(bashrc, []byte(
		"# existing config\n"+
			"# Added by Lerd installer\n"+
			`export PATH="/home/user/.local/share/lerd/bin:$PATH"`+"\n",
	), 0644)

	// Point HOME at the temp dir so removeShellEntry reads our fake rc files.
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	removeShellEntry()

	got, _ := os.ReadFile(bashrc)
	if strings.Contains(string(got), "Added by Lerd installer") {
		t.Error("Lerd marker should have been removed from .bashrc")
	}
	if strings.Contains(string(got), "lerd/bin") {
		t.Error("Lerd PATH export should have been removed from .bashrc")
	}
	if !strings.Contains(string(got), "# existing config") {
		t.Error("pre-existing config should be preserved")
	}
}

func TestRemoveShellEntry_fishConfig(t *testing.T) {
	tmp := t.TempDir()
	fishDir := filepath.Join(tmp, ".config", "fish", "conf.d")
	os.MkdirAll(fishDir, 0755)

	fishConf := filepath.Join(fishDir, "lerd.fish")
	os.WriteFile(fishConf, []byte(
		"# Added by Lerd installer\n"+
			"fish_add_path /home/user/.local/share/lerd/bin\n",
	), 0644)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	removeShellEntry()

	got, _ := os.ReadFile(fishConf)
	if strings.Contains(string(got), "Added by Lerd installer") {
		t.Error("Lerd marker should have been removed from fish config")
	}
}

// TestRemoveShellEntry_removesAllLerdInstallerMarkers pins the fix for a
// user-reported regression: `lerd install` (the Go binary) writes two
// extra marker blocks beyond `install.sh`'s "# Added by Lerd installer"
// — "# Lerd" (PATH export) and "# Lerd completions" (fpath + autoload).
// Uninstall used to match only the first marker, leaving the other two
// behind on every install path that went through `lerd install` (which
// is the common case).
func TestRemoveShellEntry_removesAllLerdInstallerMarkers(t *testing.T) {
	tmp := t.TempDir()
	zshrc := filepath.Join(tmp, ".zshrc")
	os.WriteFile(zshrc, []byte(
		"alias gco='git checkout'\n"+
			"\n"+
			"# Added by Lerd installer\n"+
			`export PATH="/h/u/.local/bin:$PATH"`+"\n"+
			"\n"+
			"# Lerd\n"+
			`export PATH="/h/u/.local/share/lerd/bin:$PATH"`+"\n"+
			"\n"+
			"# Lerd completions\n"+
			"fpath=(/h/u/.local/share/zsh/site-functions $fpath)\n"+
			"autoload -Uz compinit && compinit\n"+
			"\n"+
			"alias home='cd /h/u'\n",
	), 0644)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	removeShellEntry()

	got, _ := os.ReadFile(zshrc)
	for _, leftover := range []string{
		"Added by Lerd installer",
		"# Lerd\n",
		"# Lerd completions",
		"share/lerd/bin",
		"share/zsh/site-functions",
		"compinit",
	} {
		if strings.Contains(string(got), leftover) {
			t.Errorf("expected %q to be removed; remaining content:\n%s", leftover, got)
		}
	}
	for _, preserved := range []string{"git checkout", "cd /h/u"} {
		if !strings.Contains(string(got), preserved) {
			t.Errorf("expected %q to be preserved", preserved)
		}
	}
}

// TestRemoveShellEntry_fishFileDeletedWhenEmpty pins that an entirely
// lerd-owned fish config file is removed (not left as an empty
// conf.d entry that fish keeps sourcing on every shell start).
func TestRemoveShellEntry_fishFileDeletedWhenEmpty(t *testing.T) {
	tmp := t.TempDir()
	fishDir := filepath.Join(tmp, ".config", "fish", "conf.d")
	os.MkdirAll(fishDir, 0755)
	fishConf := filepath.Join(fishDir, "lerd.fish")
	os.WriteFile(fishConf, []byte(
		"\n# Added by Lerd installer\nfish_add_path /h/u/.local/bin\n",
	), 0644)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	removeShellEntry()

	if _, err := os.Stat(fishConf); !os.IsNotExist(err) {
		body, _ := os.ReadFile(fishConf)
		t.Errorf("expected lerd.fish to be removed; still exists with content:\n%s", body)
	}
}

// TestRemoveShellEntry_fishFileKeptWhenNonEmpty pins the inverse: if
// the user added their own content to lerd.fish beyond our markers, we
// strip our blocks but leave the file alone.
func TestRemoveShellEntry_fishFileKeptWhenNonEmpty(t *testing.T) {
	tmp := t.TempDir()
	fishDir := filepath.Join(tmp, ".config", "fish", "conf.d")
	os.MkdirAll(fishDir, 0755)
	fishConf := filepath.Join(fishDir, "lerd.fish")
	os.WriteFile(fishConf, []byte(
		"# Added by Lerd installer\nfish_add_path /h/u/.local/bin\n\n"+
			"# user-added: alias for personal use\nalias myls 'ls -la'\n",
	), 0644)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	removeShellEntry()

	body, err := os.ReadFile(fishConf)
	if err != nil {
		t.Fatalf("file should have been kept: %v", err)
	}
	if strings.Contains(string(body), "Added by Lerd installer") {
		t.Errorf("expected lerd marker removed, got:\n%s", body)
	}
	if !strings.Contains(string(body), "myls") {
		t.Errorf("user content lost; got:\n%s", body)
	}
}

func TestRemoveShellEntry_noRcFiles_noError(t *testing.T) {
	// Point HOME at an empty dir — no rc files exist, should not panic.
	tmp := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmp)
	defer os.Setenv("HOME", origHome)

	removeShellEntry() // must not panic
}
