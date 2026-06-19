// Package phpantom manages the phpantom_lsp PHP language server binary that
// powers tinker autocomplete, diagnostics, and hover in the web UI.
//
// phpantom_lsp (https://github.com/PHPantom-dev/phpantom_lsp) is a single,
// self-contained Rust binary: it bundles phpstorm-stubs and the Mago parser
// and needs no PHP runtime to analyze a project. That lets lerd run it on the
// host pointed at the project directory, alongside the other host tools it
// already manages (fnm, mkcert, composer) in BinDir, rather than baking it
// into the per-version PHP container images.
package phpantom

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/geodro/lerd/internal/config"
)

// Version pins the phpantom_lsp release lerd installs. Bump alongside a
// tested upgrade; the binary is re-fetched when the on-disk copy is missing.
const Version = "0.8.0"

const binName = "phpantom_lsp"

// BinPath is the managed location of the phpantom_lsp executable.
func BinPath() string {
	return filepath.Join(config.BinDir(), binName)
}

// Installed reports whether the managed binary is present.
func Installed() bool {
	info, err := os.Stat(BinPath())
	return err == nil && !info.IsDir()
}

// assetName returns the release tarball name for the host platform.
func assetName() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "phpantom_lsp-x86_64-unknown-linux-gnu.tar.gz", nil
	case "linux/arm64":
		return "phpantom_lsp-aarch64-unknown-linux-gnu.tar.gz", nil
	case "darwin/amd64":
		return "phpantom_lsp-x86_64-apple-darwin.tar.gz", nil
	case "darwin/arm64":
		return "phpantom_lsp-aarch64-apple-darwin.tar.gz", nil
	default:
		return "", fmt.Errorf("phpantom_lsp: unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func downloadURL() (string, error) {
	asset, err := assetName()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://github.com/PHPantom-dev/phpantom_lsp/releases/download/%s/%s", Version, asset), nil
}

// EnsureBinary downloads and extracts phpantom_lsp into BinDir when it is not
// already present. It is safe to call on every connection: once installed it
// returns immediately. The download honours ctx, so a caller whose request is
// cancelled (e.g. the browser tab closing mid-download) aborts the fetch.
func EnsureBinary(ctx context.Context, w io.Writer) error {
	if Installed() {
		return nil
	}
	url, err := downloadURL()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.BinDir(), 0o755); err != nil {
		return err
	}
	fmt.Fprintf(w, "Downloading phpantom_lsp %s\n", Version)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("phpantom_lsp download: HTTP %d for %s", resp.StatusCode, url)
	}
	return extractBinary(resp.Body, BinPath())
}

// extractBinary pulls the phpantom_lsp executable out of the gzipped tar
// stream and installs it at dest via an atomic rename. It extracts to a
// per-call unique temp file in the same directory so two concurrent installs
// can never interleave writes into a shared scratch path and rename a
// corrupted binary into place; the temp is always cleaned up.
func extractBinary(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("phpantom_lsp: %q not found in archive", binName)
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != binName {
			continue
		}
		tmp, err := os.CreateTemp(filepath.Dir(dest), binName+"-*.tmp")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		defer os.Remove(tmpName) // no-op once renamed; cleans up on any failure

		if _, err := io.Copy(tmp, tr); err != nil { //nolint:gosec // trusted release archive
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if err := os.Chmod(tmpName, 0o755); err != nil {
			return err
		}
		return os.Rename(tmpName, dest)
	}
}
