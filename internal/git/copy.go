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

// InstallDependencies runs composer install and npm ci in projectPath so
// vendor/ and node_modules/ match that checkout's own lockfiles. Uses the
// lerd composer and npm shims from BinDir so the commands route through
// the project's PHP-FPM container and the correct Node version.
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
		npm := filepath.Join(config.BinDir(), "npm")
		cmd := "install"
		if hasFile(projectPath, "package-lock.json") || hasFile(projectPath, "npm-shrinkwrap.json") {
			cmd = "ci"
		}
		if err := runIn(projectPath, npm, cmd, "--no-progress"); err != nil {
			errs = append(errs, fmt.Errorf("npm %s: %w", cmd, err))
		}
	}

	return errors.Join(errs...)
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
