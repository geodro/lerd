package git

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/geodro/lerd/internal/config"
)

// CopyTree copies src to dst recursively. It first tries a reflink-aware
// fast path via cp, which is near-instant on btrfs, XFS with reflink=1,
// and APFS. Falls back to a plain recursive Go copy elsewhere. dst must
// not already exist.
func CopyTree(src, dst string) error {
	if err := copyTreeCP(src, dst); err == nil {
		return nil
	}
	_ = os.RemoveAll(dst)
	return copyTreeNative(src, dst)
}

func copyTreeCP(src, dst string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("cp", "-a", "--reflink=auto", src, dst)
	case "darwin":
		cmd = exec.Command("cp", "-Rc", src, dst)
	default:
		return errors.New("reflink path unsupported on " + runtime.GOOS)
	}
	return cmd.Run()
}

func copyTreeNative(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		return copyFileWithMode(path, target, info.Mode())
	})
}

func copyFileWithMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// InstallDependencies runs composer install and the JS package manager
// matching whatever lockfile the project ships so vendor/ and node_modules/
// match that checkout's own lockfiles. composer goes through the lerd
// shim (which routes into the project's PHP-FPM container); JS tooling
// goes through whichever of pnpm/yarn/bun/npm is on PATH, preferring the
// npm shim from BinDir when the project uses npm so the fnm Node version
// is picked up.
//
// Each install is skipped when the relevant install marker
// (vendor/composer/installed.json for composer, the package manager's own
// marker under node_modules for JS) is at-or-newer than the lockfile —
// nothing has drifted, so re-running install would just burn cycles on
// post-autoload-dump scripts (Laravel package:discover, Filament asset
// publish, etc.) for no observable change.
//
// Errors are aggregated and returned; callers should log them rather than
// treat them as fatal since the worktree is still usable with the copied
// trees from main.
func InstallDependencies(projectPath string) error {
	var errs []error

	if composerNeedsInstall(projectPath) {
		composer := filepath.Join(config.BinDir(), "composer")
		if err := runIn(projectPath, composer, "install", "--no-interaction", "--no-progress"); err != nil {
			errs = append(errs, fmt.Errorf("composer install: %w", err))
		}
	}

	if jsNeedsInstall(projectPath) {
		if err := runJSInstall(projectPath); err != nil {
			errs = append(errs, err)
		}
	}

	// `npm run build` is intentionally NOT called here. The watcher invokes
	// InstallDependencies on every worktree add, and a build is heavy
	// (5-30s+), can fail silently, and is project-specific in a way the
	// universal install steps are not. `lerd worktree add` triggers the
	// build explicitly via RunFrontendBuild after dependencies are in place.

	return errors.Join(errs...)
}

// RunNpmScript executes `<package-manager> run <script>` in projectPath,
// using the lerd npm shim for npm projects so fnm's current Node version
// wins and falling back to PATH for pnpm/yarn/bun. Exported for callers like
// `lerd worktree add` that opt into a build step interactively. out receives
// both stdout and stderr from the build; pass os.Stdout for CLI use or an
// SSE writer to surface vite/webpack failures in the dashboard modal.
func RunNpmScript(projectPath, script string, out io.Writer) error {
	name, _ := jsPackageManager(projectPath)
	var bin string
	if name == "npm" {
		bin = filepath.Join(config.BinDir(), "npm")
	} else if p, ok := lookJSBin(name); ok {
		bin = p
	} else {
		return fmt.Errorf("%s not found on PATH", name)
	}
	if out == nil {
		out = os.Stdout
	}
	cmd := exec.Command(bin, "run", script)
	cmd.Dir = projectPath
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

// composerNeedsInstall reports whether composer install must run for the
// project at projectPath. It returns false when there is no composer.json,
// when vendor/composer/installed.json exists and is at-or-newer than
// composer.lock (or composer.json when there is no lockfile yet), and
// true otherwise.
func composerNeedsInstall(projectPath string) bool {
	if !hasFile(projectPath, "composer.json") {
		return false
	}
	marker := filepath.Join(projectPath, "vendor", "composer", "installed.json")
	ref := filepath.Join(projectPath, "composer.lock")
	if !hasFile(projectPath, "composer.lock") {
		ref = filepath.Join(projectPath, "composer.json")
	}
	return markerStale(marker, ref)
}

// jsNeedsInstall reports whether the JS package install must run for the
// project at projectPath. False when there is no package.json or when the
// package manager's install marker under node_modules/ is at-or-newer than
// the lockfile; true otherwise.
func jsNeedsInstall(projectPath string) bool {
	if !hasFile(projectPath, "package.json") {
		return false
	}
	marker, ref := jsInstallPaths(projectPath)
	return markerStale(marker, ref)
}

// jsInstallPaths returns the absolute paths to the install marker and the
// lockfile reference for whichever JS package manager owns the project, in
// the same preference order as jsPackageManager. The marker for npm is the
// .package-lock.json snapshot npm writes inside node_modules; for pnpm we
// use .modules.yaml, for yarn install-state.gz, and for bun the package
// manager's own per-install lockfile inside node_modules. When no lockfile
// is present the package.json itself is the reference (a manifest edit is
// the only thing that can require a re-install).
func jsInstallPaths(projectPath string) (marker, ref string) {
	nm := filepath.Join(projectPath, "node_modules")
	switch {
	case hasFile(projectPath, "pnpm-lock.yaml"):
		return filepath.Join(nm, ".modules.yaml"), filepath.Join(projectPath, "pnpm-lock.yaml")
	case hasFile(projectPath, "yarn.lock"):
		return filepath.Join(projectPath, ".yarn", "install-state.gz"), filepath.Join(projectPath, "yarn.lock")
	case hasFile(projectPath, "bun.lockb"):
		return filepath.Join(nm, ".bun-tag"), filepath.Join(projectPath, "bun.lockb")
	case hasFile(projectPath, "bun.lock"):
		return filepath.Join(nm, ".bun-tag"), filepath.Join(projectPath, "bun.lock")
	case hasFile(projectPath, "package-lock.json"):
		return filepath.Join(nm, ".package-lock.json"), filepath.Join(projectPath, "package-lock.json")
	case hasFile(projectPath, "npm-shrinkwrap.json"):
		return filepath.Join(nm, ".package-lock.json"), filepath.Join(projectPath, "npm-shrinkwrap.json")
	}
	return filepath.Join(nm, ".package-lock.json"), filepath.Join(projectPath, "package.json")
}

// markerStale returns true when the marker file is missing or older than
// the reference file. A missing reference (lockfile/manifest) is treated
// as "no signal" and the marker is trusted: in that pathological case we
// avoid spurious reinstalls.
func markerStale(marker, ref string) bool {
	refInfo, err := os.Stat(ref)
	if err != nil {
		return false
	}
	markerInfo, err := os.Stat(marker)
	if err != nil {
		return true
	}
	return markerInfo.ModTime().Before(refInfo.ModTime())
}

// jsPackageManager returns the name and install args for the package
// manager a project uses, picked from the presence of lockfiles.
// Preference order mirrors each manager's lockfile being definitive:
// pnpm-lock.yaml ▸ yarn.lock ▸ bun.lock(b) ▸ npm lockfile ▸ npm as fallback.
func jsPackageManager(projectPath string) (name string, args []string) {
	switch {
	case hasFile(projectPath, "pnpm-lock.yaml"):
		return "pnpm", []string{"install", "--frozen-lockfile"}
	case hasFile(projectPath, "yarn.lock"):
		// --immutable covers both yarn classic (v1) and berry (v2+); v1
		// doesn't understand it but falls back to default install, which
		// is what we want if the lockfile is already present.
		return "yarn", []string{"install", "--immutable"}
	case hasFile(projectPath, "bun.lockb"), hasFile(projectPath, "bun.lock"):
		return "bun", []string{"install", "--frozen-lockfile"}
	case hasFile(projectPath, "package-lock.json"), hasFile(projectPath, "npm-shrinkwrap.json"):
		return "npm", []string{"ci", "--no-progress"}
	default:
		return "npm", []string{"install", "--no-progress"}
	}
}

// runJSInstall resolves the chosen package manager's binary and runs the
// install. For npm we use the lerd shim from BinDir so fnm's current Node
// version wins; other managers go through PATH since lerd doesn't shim
// them. Missing binary is logged and returned so the caller aggregates it
// with other setup errors.
func runJSInstall(projectPath string) error {
	name, args := jsPackageManager(projectPath)

	var bin string
	if name == "npm" {
		bin = filepath.Join(config.BinDir(), "npm")
	} else if p, ok := lookJSBin(name); ok {
		bin = p
	} else {
		return fmt.Errorf("%s (lockfile present) not found on PATH — install it to hydrate node_modules", name)
	}

	if err := runIn(projectPath, bin, args...); err != nil {
		return fmt.Errorf("%s %s: %w", name, args[0], err)
	}
	return nil
}

func hasFile(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func runIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// lookJSBin resolves a JS package-manager binary (pnpm, yarn, bun) honoring
// PATH first, then falling back to well-known macOS package-manager prefixes
// when the lerd-watcher daemon runs under launchd's restricted PATH.
func lookJSBin(name string) (string, bool) {
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	if runtime.GOOS != "darwin" {
		return "", false
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}
