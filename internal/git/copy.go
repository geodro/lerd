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
// Errors are aggregated and returned; callers should log them rather than
// treat them as fatal since the worktree is still usable with the copied
// trees from main.
func InstallDependencies(projectPath string) error {
	var errs []error

	if hasFile(projectPath, "composer.json") {
		composer := filepath.Join(config.BinDir(), "composer")
		if err := runIn(projectPath, composer, "install", "--no-interaction", "--no-progress"); err != nil {
			errs = append(errs, fmt.Errorf("composer install: %w", err))
		}
	}

	if hasFile(projectPath, "package.json") {
		if err := runJSInstall(projectPath); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
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
	} else if p, err := exec.LookPath(name); err == nil {
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
