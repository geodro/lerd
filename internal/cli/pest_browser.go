package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// pestBrowserPkg is the Alpine package baked into the shared FPM image so
// Playwright has a musl-native browser to drive. Playwright's own downloaded
// Chromium is a glibc binary and cannot run on Alpine's musl libc.
const pestBrowserPkg = "chromium"

// pestBrowserCachePath is where the Playwright registry and lerd's chromium
// shims live: a persistent volume, baked into the image as PLAYWRIGHT_BROWSERS_PATH
// (see podman.PlaywrightCachePath / buildCustomPackagesBlock) so `lerd test`/
// `lerd pest` find the browsers regardless of the host HOME they exec with.
const pestBrowserCachePath = podman.PlaywrightCachePath

// playwrightBinRel is the project-relative path to the locally installed
// Playwright CLI, used both to download the registry and to fail fast before any
// expensive work when the npm package is missing.
const playwrightBinRel = "node_modules/.bin/playwright"

// pestBrowserShim rewrites Playwright's downloaded (glibc) browser binaries to a
// wrapper that execs the system musl chromium. Pest hardcodes its launch options
// and exposes no executablePath hook, so the wrapper is the only lever. It
// injects --no-sandbox (required for chromium as root in a container, which Pest
// never passes) and forces HOME=/root: `lerd test`/`lerd pest` exec with the
// host HOME, and musl chromium crashes writing its config into the bind-mounted
// host home, so the browser needs an isolated, writable home. The find globs the
// cache (an undocumented Playwright layout, but the only handle Pest leaves us),
// so it stays correct across browser revisions; if a future Playwright renames
// the binaries the install fails loudly via `test "$n" -gt 0`.
var pestBrowserShim = fmt.Sprintf(`set -e
cache="${PLAYWRIGHT_BROWSERS_PATH:-%s}"
if [ ! -d "$cache" ]; then echo "no Playwright browser cache at $cache" >&2; exit 1; fi
find "$cache" -type f \( -name chrome-headless-shell -o -name chrome \) -print0 2>/dev/null | while IFS= read -r -d '' b; do
  printf '#!/bin/sh\nexport HOME=/root\nexec /usr/bin/chromium --no-sandbox "$@"\n' > "$b"
  chmod +x "$b"
done
n=$(find "$cache" -type f \( -name chrome-headless-shell -o -name chrome \) 2>/dev/null | wc -l)
echo "  shimmed $n browser binary(ies) to system chromium"
test "$n" -gt 0
`, pestBrowserCachePath)

// pestBrowserInstall runs the project's Playwright CLI to populate the browser
// registry, preferring the locally installed binary so the downloaded revision
// matches the project's pinned Playwright version. The download itself is mostly
// wasted work (the glibc binaries it fetches are immediately overwritten by the
// shim), but it is the canonical, version-proof way to create the exact cache
// layout Playwright later looks for, so we accept the cost over reconstructing
// that layout by hand.
const pestBrowserInstall = `if [ -x ./node_modules/.bin/playwright ]; then
  ./node_modules/.bin/playwright install chromium
else
  echo "the 'playwright' npm package is not in node_modules — run: lerd npm install playwright" >&2
  exit 1
fi
`

// pestBrowserSupportedVersion rejects the frozen legacy PHP tier, whose base
// image ships a Node too old for current Playwright (see docs). Returning early
// here avoids a multi-minute image rebuild that would only fail later.
func pestBrowserSupportedVersion(version string) error {
	if IsLegacyPHPVersion(version) {
		return fmt.Errorf("Pest browser testing is not supported on the legacy PHP %s tier (its bundled Node is too old for Playwright); use a current PHP version", version)
	}
	return nil
}

// NewPestBrowserCmd returns the pest:browser parent command, which sets up
// in-container Pest browser testing. Pest drives Playwright locally where the
// tests run, so the browser must live in the PHP-FPM container. This bakes
// Alpine's musl chromium into the shared image (opt-in, like php:pkg) and shims
// Playwright's glibc browser to it, so no per-site Containerfile is needed.
func NewPestBrowserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pest:browser",
		Short: "Set up in-container Pest browser testing (Playwright on musl chromium)",
	}
	cmd.AddCommand(newPestBrowserInstallCmd())
	cmd.AddCommand(newPestBrowserRemoveCmd())
	cmd.AddCommand(newPestBrowserDoctorCmd())
	return cmd
}

func newPestBrowserInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install [php-version]",
		Short: "Bake musl chromium into the FPM image and wire up Playwright for Pest",
		Long: "Adds chromium to the shared PHP-FPM image (the same package mechanism as php:pkg), rebuilds it, " +
			"downloads the Playwright browser registry into a persistent volume, and shims Playwright's glibc " +
			"browser to the musl chromium so `pestphp/pest-plugin-browser` runs inside the normal FPM container. " +
			"Re-run it after bumping the Playwright version in your project.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			version, err := phpExtVersion(args)
			if err != nil {
				return err
			}
			return installPestBrowser(version, os.Stdout)
		},
	}
}

// playwrightVolumeMounted reports whether the persistent Playwright cache volume
// is already mounted in the running container, so reruns only restart it when
// the mount is genuinely missing (a container created before this shipped).
func playwrightVolumeMounted(container string) bool {
	return fpmVolumeMounted(container, pestBrowserCachePath)
}

// playwrightEnvBaked reports whether the image carries PLAYWRIGHT_BROWSERS_PATH,
// which `lerd test`/`lerd pest` rely on to find the browsers in the volume
// regardless of the host HOME they exec with.
func playwrightEnvBaked(container string) bool {
	out, err := podman.Cmd("exec", container, "printenv", "PLAYWRIGHT_BROWSERS_PATH").Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

func installPestBrowser(version string, w io.Writer) error {
	if err := pestBrowserSupportedVersion(version); err != nil {
		return err
	}
	container := fpmContainerName(version)
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "Setting up Pest browser testing for PHP %s...\n", version)

	if !config.ComposerHasPackage(cwd, "pestphp/pest-plugin-browser") {
		fmt.Fprintln(w, "  [note] pestphp/pest-plugin-browser is not in composer.json yet.")
		fmt.Fprintln(w, "         Add it with: lerd composer require --dev pestphp/pest-plugin-browser")
	}

	// Fail fast before mutating config or paying for a rebuild: the registry
	// download below needs the project's Playwright CLI.
	if _, statErr := os.Stat(filepath.Join(cwd, playwrightBinRel)); statErr != nil {
		return fmt.Errorf("the playwright npm package is not installed in this project — run `lerd npm install playwright` first")
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	added := false
	if !slices.Contains(cfg.GetPackages(version), pestBrowserPkg) {
		cfg.AddPackage(version, pestBrowserPkg)
		if err := config.SaveGlobal(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		added = true
	}

	if err := podman.WriteFPMQuadlet(version); err != nil {
		return fmt.Errorf("updating FPM quadlet: %w", err)
	}

	if running, _ := podman.ContainerRunning(container); !running {
		return fmt.Errorf("PHP %s FPM container is not running — start it with: %s", version, serviceStartHint(container))
	}

	// Rebuild when chromium was just opted in, or when an image from an older
	// install lacks the baked PLAYWRIGHT_BROWSERS_PATH the test runner relies on.
	needRebuild := added || !playwrightEnvBaked(container)
	if needRebuild {
		fmt.Fprintf(w, "Baking chromium into the PHP %s image...\n", version)
		if err := podman.RebuildFPMImageTo(version, false, w); err != nil {
			if added {
				cfg.RemovePackage(version, pestBrowserPkg)
				_ = config.SaveGlobal(cfg)
			}
			return fmt.Errorf("rebuild failed: %w", err)
		}
	}
	if needRebuild || !playwrightVolumeMounted(container) {
		fmt.Fprintf(w, "Restarting the PHP %s container...\n", version)
		if err := restartFPMAndWait(container); err != nil {
			return err
		}
	}

	out, err := podman.Cmd("exec", container, "chromium", "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("chromium did not run in the PHP %s container after rebuild: %w\n%s", version, err, out)
	}
	fmt.Fprintf(w, "  %s\n", strings.TrimSpace(string(out)))

	browsersEnv := "PLAYWRIGHT_BROWSERS_PATH=" + pestBrowserCachePath
	fmt.Fprintln(w, "Downloading the Playwright browser registry...")
	inst := podman.Cmd("exec", "-w", cwd, "--env", browsersEnv, container, "sh", "-c", pestBrowserInstall)
	inst.Stdout = w
	inst.Stderr = w
	if err := inst.Run(); err != nil {
		return fmt.Errorf("playwright install: %w", err)
	}

	shim := podman.Cmd("exec", "--env", browsersEnv, container, "sh", "-c", pestBrowserShim)
	shim.Stdout = w
	shim.Stderr = w
	if err := shim.Run(); err != nil {
		return fmt.Errorf("shimming Playwright browsers to system chromium: %w", err)
	}

	fmt.Fprintf(w, "\nPest browser testing is ready for PHP %s. Run your suite with `lerd test` or `lerd pest`.\n", version)
	return nil
}

func newPestBrowserRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [php-version]",
		Short: "Remove chromium from the FPM image and disable Pest browser testing",
		Long: "Drops chromium from the shared PHP-FPM image and rebuilds it, which also removes the baked " +
			"PLAYWRIGHT_BROWSERS_PATH. The persistent Playwright cache volume is left intact (it may be shared " +
			"with other PHP versions); delete it manually to reclaim disk.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			version, err := phpExtVersion(args)
			if err != nil {
				return err
			}
			return removePestBrowser(version, os.Stdout)
		},
	}
}

func removePestBrowser(version string, w io.Writer) error {
	container := fpmContainerName(version)

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	if !slices.Contains(cfg.GetPackages(version), pestBrowserPkg) {
		fmt.Fprintf(w, "Pest browser testing is not enabled for PHP %s — nothing to remove.\n", version)
		return nil
	}
	cfg.RemovePackage(version, pestBrowserPkg)
	if err := config.SaveGlobal(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Fprintf(w, "Removing chromium from the PHP %s image...\n", version)
	if err := podman.RebuildFPMImageTo(version, false, w); err != nil {
		// Restore config so it doesn't claim chromium is gone while the live
		// image still carries it (mirrors installPestBrowser's revert).
		cfg.AddPackage(version, pestBrowserPkg)
		_ = config.SaveGlobal(cfg)
		return fmt.Errorf("rebuild failed (config restored): %w", err)
	}
	if running, _ := podman.ContainerRunning(container); running {
		if err := restartFPMAndWait(container); err != nil {
			return err
		}
	}

	fmt.Fprintf(w, "Pest browser testing removed for PHP %s. The Playwright cache volume is left intact; delete %s to reclaim disk.\n", version, podman.PlaywrightVolumeDir())
	return nil
}

func newPestBrowserDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [php-version]",
		Short: "Diagnose the Pest browser testing setup for a PHP version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			version, err := phpExtVersion(args)
			if err != nil {
				return err
			}
			return doctorPestBrowser(version, os.Stdout)
		},
	}
}

func doctorPestBrowser(version string, w io.Writer) error {
	container := fpmContainerName(version)
	cwd, _ := os.Getwd()

	check := func(ok bool, label, fix string) {
		mark := "x"
		if ok {
			mark = "✓"
		}
		fmt.Fprintf(w, "  [%s] %s\n", mark, label)
		if !ok && fix != "" {
			fmt.Fprintf(w, "        → %s\n", fix)
		}
	}

	fmt.Fprintf(w, "Pest browser testing — PHP %s:\n", version)

	check(config.ComposerHasPackage(cwd, "pestphp/pest-plugin-browser"),
		"pestphp/pest-plugin-browser in composer.json",
		"lerd composer require --dev pestphp/pest-plugin-browser")

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	check(slices.Contains(cfg.GetPackages(version), pestBrowserPkg),
		"chromium baked into the FPM image", "lerd pest:browser install")

	running, _ := podman.ContainerRunning(container)
	check(running, "PHP "+version+" FPM container is running", serviceStartHint(container))
	if !running {
		return nil
	}

	chromiumOK := podman.Cmd("exec", container, "chromium", "--version").Run() == nil
	check(chromiumOK, "chromium present in the container", "lerd pest:browser install")

	playwrightOK := podman.Cmd("exec", "-w", cwd, container, "sh", "-c", "test -x ./node_modules/.bin/playwright").Run() == nil
	check(playwrightOK, "playwright npm package installed", "lerd npm install playwright")

	shimOK := podman.Cmd("exec", container, "sh", "-c",
		`fs=$(find "${PLAYWRIGHT_BROWSERS_PATH:-`+pestBrowserCachePath+`}" -type f \( -name chrome-headless-shell -o -name chrome \) 2>/dev/null); [ -n "$fs" ] || exit 1; for b in $fs; do head -1 "$b" | grep -q '#!/bin/sh' || exit 1; done`).Run() == nil
	check(shimOK, "Playwright browser shimmed to musl chromium", "lerd pest:browser install")

	return nil
}
