package podman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInteractiveShellScript_PrefersZsh(t *testing.T) {
	got := InteractiveShellScript()
	if !strings.HasPrefix(got, "command -v zsh ") {
		t.Errorf("chain should start with zsh, got: %s", got)
	}
	if !strings.Contains(got, "exec bash") {
		t.Errorf("chain must include bash fallback, got: %s", got)
	}
	if !strings.HasSuffix(got, "exec sh") {
		t.Errorf("chain must end with sh fallback, got: %s", got)
	}
}

func TestZshHistoryDir_CreatedAndScoped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	dir := zshHistoryDir("84")
	if dir == "" {
		t.Fatalf("zshHistoryDir returned empty path")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("zsh history dir should be created: %v", err)
	}
	if !strings.Contains(dir, "shell-state/php-84/zsh") {
		t.Errorf("path should be scoped per PHP version, got: %s", dir)
	}
}

func TestHostNameLine_ValidHostnameRenders(t *testing.T) {
	got := hostNameLine()
	if got == "" {
		t.Skip("host hostname unreadable or has unusual characters; nothing to assert")
	}
	if !strings.HasPrefix(got, "HostName=") {
		t.Errorf("expected HostName= prefix, got %q", got)
	}
}

func TestApplyShellMounts_RendersZshHistoryDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	tmpl := "Volume=before\nVolume={{.ZshHistoryDir}}:/root/.zsh_state:rw\n"
	got := applyShellMounts(tmpl, "84")
	if !strings.Contains(got, "/root/.zsh_state:rw") {
		t.Errorf("zsh history volume missing:\n%s", got)
	}
	if strings.Contains(got, "{{.ZshHistoryDir}}") {
		t.Errorf("template placeholder not substituted:\n%s", got)
	}
}

func TestApplyShellMounts_RendersBunVolume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	tmpl := "Volume={{.BunVolumeDir}}:/root/.bun:rw\n"
	got := applyShellMounts(tmpl, "84")
	if strings.Contains(got, "{{.BunVolumeDir}}") {
		t.Errorf("bun volume placeholder not substituted:\n%s", got)
	}
	if !strings.Contains(got, ":/root/.bun:rw") {
		t.Errorf("bun volume missing:\n%s", got)
	}
	if _, err := os.Stat(BunVolumeDir()); err != nil {
		t.Errorf("bun volume host dir should be created: %v", err)
	}
}

// The fpm container template must carry the bun volume placeholder so the
// in-container `lerd php:bun install` target has a persistent home.
func TestFPMTemplateHasBunVolume(t *testing.T) {
	tmpl, err := GetQuadletTemplate("lerd-php-fpm.container.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tmpl, "{{.BunVolumeDir}}:/root/.bun:rw") {
		t.Error("fpm template missing bun volume mount")
	}
}

func TestApplyShellMounts_RendersPlaywrightVolume(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	tmpl := "Volume={{.PlaywrightVolumeDir}}:/root/.cache/ms-playwright:rw\n"
	got := applyShellMounts(tmpl, "84")
	if strings.Contains(got, "{{.PlaywrightVolumeDir}}") {
		t.Errorf("playwright volume placeholder not substituted:\n%s", got)
	}
	if !strings.Contains(got, ":/root/.cache/ms-playwright:rw") {
		t.Errorf("playwright volume missing:\n%s", got)
	}
	if _, err := os.Stat(PlaywrightVolumeDir()); err != nil {
		t.Errorf("playwright volume host dir should be created: %v", err)
	}
}

// The fpm container template must carry the Playwright cache volume so opt-in
// Pest browser testing keeps its registry and chromium shims across rebuilds.
func TestFPMTemplateHasPlaywrightVolume(t *testing.T) {
	tmpl, err := GetQuadletTemplate("lerd-php-fpm.container.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tmpl, "{{.PlaywrightVolumeDir}}:"+PlaywrightCachePath+":rw") {
		t.Errorf("fpm template missing playwright volume mount at %s", PlaywrightCachePath)
	}
}
