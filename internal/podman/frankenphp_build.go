package podman

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// frankenPHPRuntimeExtensions is the standard PHP extension set baked into the
// derived FrankenPHP image, mirroring the runtime extensions the lerd FPM image
// ships so an Octane site has the same modules available instead of the bare
// dunglas base. These are install-php-extensions names; curl/mbstring/xml are
// already in the base image. Dev-only tooling (xdebug, pcov, spx, lerd_devtools)
// is intentionally excluded — it carries octane-specific behaviour and is
// tracked separately.
var frankenPHPRuntimeExtensions = []string{
	"bcmath", "bz2", "calendar", "dba", "exif", "gd", "gmp", "intl", "ldap",
	"mysqli", "opcache", "pcntl", "pdo_mysql", "pdo_pgsql", "shmop", "soap",
	"sockets", "sysvmsg", "sysvsem", "sysvshm", "xsl", "zip",
	"redis", "igbinary", "imagick", "mongodb",
}

// frankenPHPContainerfileHashLabel stamps the derived image with the hash of the
// Containerfile + extension list it was built from, so NeedsFrankenPHPRebuild can
// tell an up-to-date image from one a newer lerd would build differently.
const frankenPHPContainerfileHashLabel = "dev.lerd.frankenphp.containerfile-hash"

// validExtName guards extension names interpolated into the build command so a
// stray config value can't inject extra shell/build arguments.
var validExtName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// FrankenPHPImageName returns the local derived image tag for a PHP version,
// e.g. "localhost/lerd-frankenphp84:local" for "8.4".
func FrankenPHPImageName(version string) string {
	return "localhost/lerd-frankenphp" + strings.ReplaceAll(version, ".", "") + ":local"
}

// frankenPHPContainerfileHash hashes the embedded Containerfile template plus the
// baked extension list, so a change to either drifts the hash and triggers a
// rebuild after a lerd update.
func frankenPHPContainerfileHash() (string, error) {
	tmpl, err := GetQuadletTemplate("lerd-frankenphp.Containerfile")
	if err != nil {
		return "", err
	}
	// Fold in the lerd_devtools source hash so a change to that extension drifts
	// the image hash and rebuilds, the same guarantee the FPM marker line gives.
	dt, err := devtoolsSourceHash()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(tmpl + "\n" + strings.Join(frankenPHPRuntimeExtensions, " ") + "\n" + dt))
	return hex.EncodeToString(sum[:]), nil
}

// NeedsFrankenPHPRebuild reports whether any active FrankenPHP version's derived
// image is missing or stamped with a different Containerfile hash than the
// current binary builds, so a lerd update that changes the template or extension
// set rebuilds the image. False when nothing is stale.
func NeedsFrankenPHPRebuild(activeVersions []string) bool {
	current, err := frankenPHPContainerfileHash()
	if err != nil {
		return true
	}
	for _, v := range activeVersions {
		if imageLabelFn(FrankenPHPImageName(v), frankenPHPContainerfileHashLabel) != current {
			return true
		}
	}
	return false
}

// BuildFrankenPHPImage builds the derived FrankenPHP image for version, baking
// the standard extension set plus any user-configured custom extensions and
// packages. When force is false it no-ops if a current image already exists.
func BuildFrankenPHPImage(version string, force bool, w io.Writer) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	exts := cfg.GetExtensions(version)
	packages := cfg.GetPackages(version)
	// Custom extensions may need extra Alpine packages to compile;
	// install-php-extensions only auto-resolves deps for extensions it knows, so
	// fold the user-configured ext apk deps into the package set the way the FPM
	// build does, otherwise a custom extension that builds on FPM fails here.
	for _, ext := range exts {
		packages = append(packages, cfg.GetExtApkDeps(ext)...)
	}
	return buildFrankenPHPImage(version, force, exts, packages, w)
}

func buildFrankenPHPImage(version string, force bool, customExts, packages []string, w io.Writer) error {
	version = config.NormalizeFrankenPHPVersion(version)
	imageName := FrankenPHPImageName(version)

	// Compute the Containerfile hash once and use it for both the freshness check
	// and the image label, instead of re-hashing via NeedsFrankenPHPRebuild.
	hash, err := frankenPHPContainerfileHash()
	if err != nil {
		return fmt.Errorf("hashing FrankenPHP Containerfile: %w", err)
	}
	if !force {
		if exec.Command(PodmanBin(), "image", "exists", imageName).Run() == nil &&
			imageLabelFn(imageName, frankenPHPContainerfileHashLabel) == hash {
			return nil // image exists and is current
		}
	}

	fmt.Fprintf(w, "\n  Building FrankenPHP PHP %s image...\n", version)

	tmp, err := os.MkdirTemp("", "lerd-frankenphp-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	// Stage the lerd_devtools C source into the build context so the
	// `COPY internal/podman/devtools` in the Containerfile resolves.
	if err := writeDevtoolsSource(tmp); err != nil {
		return fmt.Errorf("staging devtools source: %w", err)
	}

	exts := append(append([]string{}, frankenPHPRuntimeExtensions...), sanitizeExtNames(customExts)...)
	containerfile, err := renderFrankenPHPContainerfile(version, exts, packages, mkcertCABlock(tmp))
	if err != nil {
		return err
	}

	cfPath := filepath.Join(tmp, "Containerfile")
	if err := os.WriteFile(cfPath, []byte(containerfile), 0644); err != nil {
		return err
	}

	args := []string{"build", "-t", imageName, "--label", frankenPHPContainerfileHashLabel + "=" + hash}
	if force {
		args = append(args, "--no-cache")
	}
	args = append(args, "-f", cfPath, tmp)
	cmd := exec.Command(PodmanBin(), args...)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building FrankenPHP PHP %s image: %w", version, err)
	}
	fmt.Fprintf(w, "  FrankenPHP PHP %s image built successfully.\n", version)
	return nil
}

// renderFrankenPHPContainerfile substitutes the embedded template with the PHP
// version, the full extension list (standard + custom), the user's extra
// packages, and the mkcert CA block. Pure, so the build's image definition has
// unit-test coverage without invoking podman.
func renderFrankenPHPContainerfile(version string, exts, packages []string, mkcertBlock string) (string, error) {
	tmpl, err := GetQuadletTemplate("lerd-frankenphp.Containerfile")
	if err != nil {
		return "", err
	}
	dt, err := devtoolsSourceHash()
	if err != nil {
		return "", err
	}
	cf := strings.ReplaceAll(tmpl, "{{.Version}}", version)
	cf = strings.ReplaceAll(cf, "{{.Extensions}}", strings.Join(exts, " "))
	cf = strings.ReplaceAll(cf, "{{.DevtoolsHash}}", dt)
	cf = strings.ReplaceAll(cf, "{{.CustomPackages}}", buildCustomPackagesBlock(packages))
	cf = strings.ReplaceAll(cf, "{{.MkcertCA}}", mkcertBlock)
	return cf, nil
}

// sanitizeExtNames keeps only well-formed extension tokens, dropping anything a
// stray config value might smuggle in.
func sanitizeExtNames(in []string) []string {
	var out []string
	for _, e := range in {
		if e = strings.TrimSpace(e); e != "" && validExtName.MatchString(e) {
			out = append(out, e)
		}
	}
	return out
}
