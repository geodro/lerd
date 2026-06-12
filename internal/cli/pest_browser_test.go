package cli

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/podman"
)

// The shim is the whole mechanism: Playwright's glibc browser can't run on
// Alpine musl, so the downloaded binaries must be rewritten to exec the system
// musl chromium with --no-sandbox (Pest never passes it). Guard those invariants.
func TestPestBrowserShim_RewritesToMuslChromium(t *testing.T) {
	for _, want := range []string{
		"/usr/bin/chromium",
		"--no-sandbox",
		"chrome-headless-shell",
		"-name chrome",
		"PLAYWRIGHT_BROWSERS_PATH",
	} {
		if !strings.Contains(pestBrowserShim, want) {
			t.Errorf("shim script missing %q:\n%s", want, pestBrowserShim)
		}
	}
}

// Install must prefer the project's pinned Playwright so the downloaded browser
// revision matches the plugin's expectation, and fail loudly when it is absent.
func TestPestBrowserInstall_PrefersLocalPlaywright(t *testing.T) {
	if !strings.Contains(pestBrowserInstall, "./node_modules/.bin/playwright") {
		t.Error("install script should use the locally installed playwright binary")
	}
	if !strings.Contains(pestBrowserInstall, "lerd npm install playwright") {
		t.Error("install script should hint how to install playwright when missing")
	}
}

func TestPestBrowserPkgIsChromium(t *testing.T) {
	if pestBrowserPkg != "chromium" {
		t.Errorf("pest:browser must bake the Alpine chromium package, got %q", pestBrowserPkg)
	}
}

// Browser testing needs a modern Node; the frozen legacy 7.4/8.0 tier must be
// rejected up front rather than failing after a multi-minute rebuild.
func TestPestBrowserSupportedVersion(t *testing.T) {
	for _, v := range []string{"7.4", "8.0"} {
		if pestBrowserSupportedVersion(v) == nil {
			t.Errorf("legacy PHP %s must be rejected for browser testing", v)
		}
	}
	for _, v := range []string{"8.3", "8.4", "8.5"} {
		if err := pestBrowserSupportedVersion(v); err != nil {
			t.Errorf("PHP %s should be supported, got %v", v, err)
		}
	}
}

// The shim must shim both browser binaries and use a NUL-delimited find so paths
// with spaces or newlines can't corrupt the rewrite.
func TestPestBrowserShim_HandlesBothBinariesSafely(t *testing.T) {
	for _, want := range []string{"-name chrome-headless-shell", "-name chrome", "-print0", "read -r -d ''"} {
		if !strings.Contains(pestBrowserShim, want) {
			t.Errorf("shim missing %q:\n%s", want, pestBrowserShim)
		}
	}
}

// The cli cache path must stay equal to the podman source of truth that bakes
// the image ENV and the volume mount target.
func TestPestBrowserCachePathMatchesPodman(t *testing.T) {
	if pestBrowserCachePath != podman.PlaywrightCachePath {
		t.Errorf("cache path drift: cli=%q podman=%q", pestBrowserCachePath, podman.PlaywrightCachePath)
	}
}

func TestNewPestBrowserCmd_HasSubcommands(t *testing.T) {
	cmd := NewPestBrowserCmd()
	if cmd.Use != "pest:browser" {
		t.Errorf("parent command Use = %q, want pest:browser", cmd.Use)
	}
	want := map[string]bool{"install": false, "remove": false, "doctor": false}
	for _, sub := range cmd.Commands() {
		name := strings.Fields(sub.Use)[0]
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("pest:browser missing %q subcommand", name)
		}
	}
}
