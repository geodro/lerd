package podman

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// mkcertPath returns the path to the mkcert binary managed by lerd.
func mkcertPath() string {
	return filepath.Join(config.BinDir(), "mkcert")
}

// mkcertCABlock copies the mkcert rootCA.pem into tmpDir and returns the
// Containerfile snippet that installs it into the Alpine trust store.
// Returns empty string if mkcert is not installed or the CA does not exist.
func mkcertCABlock(tmpDir string) string {
	out, err := exec.Command(mkcertPath(), "-CAROOT").Output()
	if err != nil {
		return ""
	}
	rootCA := filepath.Join(strings.TrimSpace(string(out)), "rootCA.pem")
	src, err := os.ReadFile(rootCA)
	if err != nil {
		return ""
	}
	dest := filepath.Join(tmpDir, "mkcert-ca.crt")
	if err := os.WriteFile(dest, src, 0644); err != nil {
		return ""
	}
	return "# Lerd mkcert CA — trust local .test HTTPS inside the container\n" +
		"COPY mkcert-ca.crt /usr/local/share/ca-certificates/mkcert-ca.crt\n" +
		"RUN update-ca-certificates\n"
}

// ContainerfileHash returns the SHA-256 hash of the embedded PHP-FPM Containerfile.
// This is used to detect when images need to be rebuilt after a lerd update.
func ContainerfileHash() (string, error) {
	tmpl, err := GetQuadletTemplate("lerd-php-fpm.Containerfile")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(tmpl))
	return fmt.Sprintf("%x", sum), nil
}

// NeedsFPMRebuild returns true if the stored Containerfile hash differs from the
// current embedded Containerfile, meaning images should be rebuilt.
func NeedsFPMRebuild() bool {
	current, err := ContainerfileHash()
	if err != nil {
		return false
	}
	stored, err := os.ReadFile(config.PHPImageHashFile())
	if err != nil {
		// No stored hash yet — treat as needing rebuild only if images exist
		return false
	}
	return strings.TrimSpace(string(stored)) != current
}

// StoreFPMHash writes the current Containerfile hash to disk.
func StoreFPMHash() error {
	hash, err := ContainerfileHash()
	if err != nil {
		return err
	}
	return os.WriteFile(config.PHPImageHashFile(), []byte(hash), 0644)
}

// BuildFPMImage builds the lerd PHP-FPM image for the given version if it doesn't exist.
// When local is false, it attempts to pull a pre-built base image from ghcr.io first.
func BuildFPMImage(version string, local bool) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	return buildFPMImage(version, false, local, cfg.GetExtensions(version), os.Stdout)
}

// BuildFPMImageTo builds the PHP-FPM image writing output to w.
// When local is false, it attempts to pull a pre-built base image from ghcr.io first.
func BuildFPMImageTo(version string, local bool, w io.Writer) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	return buildFPMImage(version, false, local, cfg.GetExtensions(version), w)
}

// RebuildFPMImage force-removes and rebuilds the PHP-FPM image for the given version.
// When local is false, it attempts to pull a pre-built base image from ghcr.io first.
func RebuildFPMImage(version string, local bool) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	return buildFPMImage(version, true, local, cfg.GetExtensions(version), os.Stdout)
}

// RebuildFPMImageTo force-rebuilds the PHP-FPM image writing output to w.
// When local is false, it attempts to pull a pre-built base image from ghcr.io first.
func RebuildFPMImageTo(version string, local bool, w io.Writer) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	return buildFPMImage(version, true, local, cfg.GetExtensions(version), w)
}

// baseContainerfileHash returns a 12-character SHA-256 prefix of the Containerfile
// with user-specific sections stripped. This is used as the tag for pre-built base
// images on ghcr.io, so lerd knows exactly which image matches its embedded template.
func baseContainerfileHash() (string, error) {
	tmpl, err := GetQuadletTemplate("lerd-php-fpm.Containerfile")
	if err != nil {
		return "", err
	}
	base := strings.ReplaceAll(tmpl, "{{.CustomExtensions}}", "")
	base = strings.ReplaceAll(base, "{{.MkcertCA}}", "")
	sum := sha256.Sum256([]byte(base))
	return fmt.Sprintf("%x", sum)[:12], nil
}

// tryPullBaseImage attempts to pull the pre-built base image from ghcr.io.
// Returns the image reference on success, or "" if unavailable.
func tryPullBaseImage(version string, w io.Writer) string {
	hash, err := baseContainerfileHash()
	if err != nil {
		return ""
	}
	short := strings.ReplaceAll(version, ".", "")
	ref := fmt.Sprintf("ghcr.io/geodro/lerd-php%s-fpm-base:%s", short, hash)
	fmt.Fprintf(w, "  Pulling pre-built PHP %s base image...\n", version)
	cmd := exec.Command("podman", "pull", ref)
	cmd.Stdout = w
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(w, "  Pre-built image unavailable, falling back to local build (may take a few minutes)...\n")
		return ""
	}
	return ref
}

func buildFPMImage(version string, force, local bool, customExts []string, w io.Writer) error {
	short := strings.ReplaceAll(version, ".", "")
	imageName := "lerd-php" + short + "-fpm:local"

	if !force {
		// Skip if image already exists
		if exec.Command("podman", "image", "exists", imageName).Run() == nil {
			return nil
		}
	}

	fmt.Fprintf(w, "\n  Building PHP %s image...\n", version)

	tmp, err := os.MkdirTemp("", "lerd-php-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	var containerfile string
	buildArgs := []string{"build", "-t", imageName}

	// Fast path: pull pre-built base and layer just mkcert CA + custom extensions on top.
	if !local {
		if baseRef := tryPullBaseImage(version, w); baseRef != "" {
			containerfile = "FROM " + baseRef + "\n" +
				buildCustomExtBlock(customExts) +
				mkcertCABlock(tmp)
			goto build
		}
	}

	// Slow path: full local build from the embedded Containerfile template.
	{
		tmpl, tmplErr := GetQuadletTemplate("lerd-php-fpm.Containerfile")
		if tmplErr != nil {
			return tmplErr
		}
		containerfile = strings.ReplaceAll(tmpl, "{{.Version}}", version)
		containerfile = strings.ReplaceAll(containerfile, "{{.CustomExtensions}}", buildCustomExtBlock(customExts))
		containerfile = strings.ReplaceAll(containerfile, "{{.MkcertCA}}", mkcertCABlock(tmp))
		if force {
			// Bypass layer cache so changes are fully applied. The old image stays
			// tagged and the container keeps running until we restart the unit.
			buildArgs = append(buildArgs, "--no-cache")
		}
	}

build:
	cfPath := filepath.Join(tmp, "Containerfile")
	if err := os.WriteFile(cfPath, []byte(containerfile), 0644); err != nil {
		return err
	}

	buildArgs = append(buildArgs, "-f", cfPath, tmp)
	cmd := exec.Command("podman", buildArgs...)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building PHP %s image: %w", version, err)
	}

	fmt.Fprintf(w, "  PHP %s image built successfully.\n", version)
	return nil
}

// buildCustomExtBlock generates Dockerfile RUN blocks for user-configured extensions.
func buildCustomExtBlock(exts []string) string {
	if len(exts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# User-configured extensions\n")
	for _, ext := range exts {
		sb.WriteString(fmt.Sprintf(
			"RUN { (pecl install %s && docker-php-ext-enable %s) || docker-php-ext-install %s || true; } \\\n    && rm -rf /tmp/pear /var/cache/apk/*\n",
			ext, ext, ext,
		))
	}
	return sb.String()
}

// WriteXdebugIni writes the per-version xdebug ini to the host config dir.
// The file is volume-mounted into the FPM container at /usr/local/etc/php/conf.d/99-xdebug.ini.
func WriteXdebugIni(version string, enabled bool) error {
	path := config.PHPConfFile(version)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	mode := "off"
	if enabled {
		mode = "debug"
	}
	content := fmt.Sprintf("[xdebug]\nxdebug.mode=%s\nxdebug.start_with_request=yes\nxdebug.client_host=host.containers.internal\nxdebug.client_port=9003\n", mode)
	return os.WriteFile(path, []byte(content), 0644)
}

// WriteFPMQuadlet writes the systemd quadlet for a PHP-FPM version and reloads the
// systemd daemon if the content changed. It also ensures the xdebug and user ini files exist.
func WriteFPMQuadlet(version string) error {
	short := strings.ReplaceAll(version, ".", "")
	unitName := "lerd-php" + short + "-fpm"

	if err := EnsureUserIni(version); err != nil {
		return fmt.Errorf("creating user ini: %w", err)
	}

	tmplContent, err := GetQuadletTemplate("lerd-php-fpm.container.tmpl")
	if err != nil {
		return err
	}
	content := strings.ReplaceAll(tmplContent, "{{.Version}}", version)
	content = strings.ReplaceAll(content, "{{.VersionShort}}", short)
	content = strings.ReplaceAll(content, "{{.XdebugIniPath}}", config.PHPConfFile(version))
	content = strings.ReplaceAll(content, "{{.UserIniPath}}", config.PHPUserIniFile(version))

	// Skip the write and daemon-reload if the quadlet is already up to date.
	// Unnecessary daemon-reloads cause Podman's quadlet generator to regenerate
	// all service files, which can briefly disrupt lerd-dns and cause
	// systemd-resolved to mark 127.0.0.1:5300 as failed (breaking .test resolution).
	existingPath := filepath.Join(config.QuadletDir(), unitName+".container")
	if existing, err := os.ReadFile(existingPath); err == nil && string(existing) == content {
		return nil
	}

	if err := WriteQuadlet(unitName, content); err != nil {
		return err
	}
	return DaemonReload()
}

// EnsureUserIni creates the per-version user php.ini with defaults if it doesn't exist.
func EnsureUserIni(version string) error {
	path := config.PHPUserIniFile(version)
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	content := "; Lerd per-version PHP settings for PHP " + version + "\n" +
		"; Edit this file, then restart: systemctl --user restart lerd-php" +
		strings.ReplaceAll(version, ".", "") + "-fpm\n" +
		";\n" +
		"; memory_limit = 512M\n" +
		"; upload_max_filesize = 64M\n" +
		"; post_max_size = 64M\n" +
		"; max_execution_time = 60\n"
	return os.WriteFile(path, []byte(content), 0644)
}
