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

// WriteContainerUnitFn writes a container unit file for the given name and content.
// Defaults to writing a systemd quadlet (.container) file.
// Override this on macOS to write a launchd plist instead.
var WriteContainerUnitFn func(name, content string) error = WriteQuadlet

// DaemonReloadFn reloads the service manager after a unit file change.
// Defaults to systemctl --user daemon-reload.
// Override this on macOS with a no-op.
var DaemonReloadFn func() error = DaemonReload

// SkipQuadletUpToDateCheck disables the early-return optimisation in
// WriteFPMQuadlet that skips writing when the .container file is unchanged.
// Set to true on macOS where the unit file is a launchd plist, not a quadlet.
var SkipQuadletUpToDateCheck bool

// ExtraVolumePaths returns absolute paths that need to be bind-mounted into the
// PHP-FPM container because they are outside the user's home directory. It
// collects parked directories and linked site paths, deduplicates them, and
// returns only the top-level ancestors (so /var/www covers /var/www/app).
func ExtraVolumePaths() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	// Ensure home has a trailing slash for prefix matching.
	homePrefix := home
	if !strings.HasSuffix(homePrefix, "/") {
		homePrefix += "/"
	}

	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || p == home || strings.HasPrefix(p, homePrefix) {
			return
		}
		seen[p] = true
	}

	if cfg, err := config.LoadGlobal(); err == nil {
		for _, dir := range cfg.ParkedDirectories {
			add(dir)
		}
	}
	if reg, err := config.LoadSites(); err == nil {
		for _, site := range reg.Sites {
			add(site.Path)
		}
	}

	if len(seen) == 0 {
		return nil
	}

	// Collect unique paths and reduce to top-level ancestors.
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	// Sort so shorter paths come first, then filter out children.
	sortPaths(paths)
	var result []string
	for _, p := range paths {
		covered := false
		for _, r := range result {
			rPrefix := r
			if !strings.HasSuffix(rPrefix, "/") {
				rPrefix += "/"
			}
			if strings.HasPrefix(p, rPrefix) || p == r {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, p)
		}
	}
	return result
}

// sortPaths sorts paths by length then lexicographically.
func sortPaths(paths []string) {
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0; j-- {
			if len(paths[j]) < len(paths[j-1]) || (len(paths[j]) == len(paths[j-1]) && paths[j] < paths[j-1]) {
				paths[j], paths[j-1] = paths[j-1], paths[j]
			}
		}
	}
}

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

	// Use an empty auth file so the pull is always anonymous, regardless of
	// whether the user is logged into ghcr.io. A logged-in account with
	// expired or mismatched credentials would otherwise cause a 401 for this
	// public image and force a slow local build.
	tmpAuth, err := os.CreateTemp("", "lerd-auth-*.json")
	if err == nil {
		tmpAuth.WriteString("{}")
		tmpAuth.Close()
		defer os.Remove(tmpAuth.Name())
	}

	args := []string{"pull", "--policy=always"}
	if tmpAuth != nil {
		args = append(args, "--authfile="+tmpAuth.Name())
	}
	args = append(args, ref)

	cmd := exec.Command(PodmanBin(), args...)
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
		if exec.Command(PodmanBin(), "image", "exists", imageName).Run() == nil {
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
			if force {
				buildArgs = append(buildArgs, "--no-cache")
			}
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
	cmd := exec.Command(PodmanBin(), buildArgs...)
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

// validXdebugModes lists the xdebug.mode tokens accepted by NormaliseXdebugMode.
// Comma-separated combinations of these are allowed (e.g. "debug,coverage");
// "off" is only valid on its own.
var validXdebugModes = map[string]bool{
	"off":      true,
	"develop":  true,
	"coverage": true,
	"debug":    true,
	"gcstats":  true,
	"profile":  true,
	"trace":    true,
}

// NormaliseXdebugMode validates and canonicalises a user-supplied xdebug.mode
// value. Whitespace is trimmed, duplicates are dropped, and the result is a
// comma-separated string ready to be written into the ini file. An empty input
// returns "debug" so callers can use it as the default when enabling xdebug
// without an explicit mode.
func NormaliseXdebugMode(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "debug", nil
	}
	parts := strings.Split(raw, ",")
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !validXdebugModes[p] {
			return "", fmt.Errorf("invalid xdebug mode %q (accepted: debug, coverage, develop, profile, trace, gcstats, off)", p)
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return "debug", nil
	}
	if len(out) > 1 && seen["off"] {
		return "", fmt.Errorf("xdebug mode %q cannot combine 'off' with other modes", raw)
	}
	return strings.Join(out, ","), nil
}

// WriteXdebugIni writes the per-version xdebug ini to the host config dir.
// The file is volume-mounted into the FPM container at /usr/local/etc/php/conf.d/99-xdebug.ini.
// An empty mode writes xdebug.mode=off (extension loaded but inactive); any other value
// is emitted as-is, so callers can pass "debug", "coverage", "debug,coverage", etc.
func WriteXdebugIni(version, mode string) error {
	path := config.PHPConfFile(version)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("removing stale xdebug ini directory: %w", err)
		}
	}
	if mode == "" {
		mode = "off"
	}
	content := fmt.Sprintf("[xdebug]\nxdebug.mode=%s\nxdebug.start_with_request=yes\nxdebug.client_host=host.containers.internal\nxdebug.client_port=9003\n", mode)
	return os.WriteFile(path, []byte(content), 0644)
}

// ensureFPMHostsFile guarantees the bind-mount source for the FPM container's
// /etc/hosts is a regular file before podman starts the container. Three states
// are normalised here:
//
//  1. Path exists and is a directory (podman auto-created it on a previous
//     broken start, same race as the xdebug ini): remove it and fall through
//     to the missing-file branch.
//  2. Path is missing: try a real WriteContainerHosts; if that fails (e.g.
//     LoadSites errors), write a minimal static header so the mount still
//     succeeds and host.containers.internal resolves to something.
//  3. Path is already a regular file: no-op.
func ensureFPMHostsFile() error {
	hostsPath := config.ContainerHostsFile()
	info, err := os.Stat(hostsPath)
	if err == nil && info.IsDir() {
		if rmErr := os.Remove(hostsPath); rmErr != nil {
			return fmt.Errorf("removing stale hosts directory: %w", rmErr)
		}
		err = os.ErrNotExist
	}
	if !os.IsNotExist(err) {
		return nil
	}
	if writeErr := WriteContainerHosts(); writeErr == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(hostsPath), 0755); err != nil {
		return err
	}
	hostIP := DetectHostGatewayIP()
	return os.WriteFile(hostsPath, []byte(
		"127.0.0.1 localhost\n"+
			"::1 localhost\n"+
			hostIP+" host.containers.internal host.docker.internal\n",
	), 0644)
}

// EnsureXdebugIni creates the xdebug ini file for the given PHP version if it doesn't
// already exist as a regular file. This prevents Podman from auto-creating a directory
// at the bind-mount source path when the container starts before the file is written.
func EnsureXdebugIni(version string) error {
	path := config.PHPConfFile(version)
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return nil // already a regular file
	}
	cfg, cfgErr := config.LoadGlobal()
	if cfgErr != nil {
		return cfgErr
	}
	return WriteXdebugIni(version, cfg.GetXdebugMode(version))
}

// WriteFPMQuadlet writes the systemd quadlet for a PHP-FPM version and reloads the
// systemd daemon if the content changed. It also ensures the xdebug and user ini files exist.
func WriteFPMQuadlet(version string) error {
	short := strings.ReplaceAll(version, ".", "")
	unitName := "lerd-php" + short + "-fpm"

	if err := EnsureUserIni(version); err != nil {
		return fmt.Errorf("creating user ini: %w", err)
	}
	if err := EnsureXdebugIni(version); err != nil {
		return fmt.Errorf("creating xdebug ini: %w", err)
	}

	if err := ensureFPMHostsFile(); err != nil {
		return err
	}

	tmplContent, err := GetQuadletTemplate("lerd-php-fpm.container.tmpl")
	if err != nil {
		return err
	}
	content := strings.ReplaceAll(tmplContent, "{{.Version}}", version)
	content = strings.ReplaceAll(content, "{{.VersionShort}}", short)
	content = strings.ReplaceAll(content, "{{.XdebugIniPath}}", config.PHPConfFile(version))
	content = strings.ReplaceAll(content, "{{.UserIniPath}}", config.PHPUserIniFile(version))
	content = InjectExtraVolumes(content, ExtraVolumePaths())

	// Skip the write and daemon-reload if the quadlet is already up to date.
	// Unnecessary daemon-reloads cause Podman's quadlet generator to regenerate
	// all service files, which can briefly disrupt lerd-dns and cause
	// systemd-resolved to mark 127.0.0.1:5300 as failed (breaking .test resolution).
	// On macOS the unit file is a launchd plist (not a quadlet), so the check is skipped.
	if !SkipQuadletUpToDateCheck {
		existingPath := filepath.Join(config.QuadletDir(), unitName+".container")
		if existing, err := os.ReadFile(existingPath); err == nil && string(existing) == content {
			return nil
		}
	}

	if err := WriteContainerUnitFn(unitName, content); err != nil {
		return err
	}
	return DaemonReloadFn()
}

// RewriteFPMQuadlets regenerates the quadlet files for all installed PHP-FPM
// versions and the nginx quadlet. Call this when parked directories or site
// paths change so that extra volume mounts stay in sync.
func RewriteFPMQuadlets() error {
	extraPaths := ExtraVolumePaths()
	versions, _ := listInstalledPHPVersions()

	var changedUnits []string

	for _, v := range versions {
		short := strings.ReplaceAll(v, ".", "")
		unitName := "lerd-php" + short + "-fpm"

		tmplContent, tmplErr := GetQuadletTemplate("lerd-php-fpm.container.tmpl")
		if tmplErr != nil {
			continue
		}
		content := strings.ReplaceAll(tmplContent, "{{.Version}}", v)
		content = strings.ReplaceAll(content, "{{.VersionShort}}", short)
		content = strings.ReplaceAll(content, "{{.XdebugIniPath}}", config.PHPConfFile(v))
		content = strings.ReplaceAll(content, "{{.UserIniPath}}", config.PHPUserIniFile(v))
		content = InjectExtraVolumes(content, extraPaths)

		changed, writeErr := WriteQuadletDiff(unitName, content)
		if writeErr != nil {
			continue
		}
		if changed {
			changedUnits = append(changedUnits, unitName)
		}
	}

	// Also rewrite nginx quadlet with the same extra volumes.
	if nginxContent, err := GetQuadletTemplate("lerd-nginx.container"); err == nil {
		nginxContent = InjectExtraVolumes(nginxContent, extraPaths)
		if changed, err := WriteQuadletDiff("lerd-nginx", nginxContent); err == nil && changed {
			changedUnits = append(changedUnits, "lerd-nginx")
		}
	}

	if len(changedUnits) > 0 {
		_ = DaemonReload()
		for _, unit := range changedUnits {
			_ = RestartUnit(unit)
		}
		// Nginx may have restarted and received a new IP. Regenerate the
		// browser-testing hosts file so Selenium resolves .test domains to
		// the current nginx container address.
		_ = WriteContainerHosts()
	}
	return nil
}

// listInstalledPHPVersions returns PHP versions that have a quadlet installed.
func listInstalledPHPVersions() ([]string, error) {
	dir := config.QuadletDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "lerd-php") || !strings.HasSuffix(name, "-fpm.container") {
			continue
		}
		// Extract version short from lerd-php84-fpm.container → "84"
		short := strings.TrimPrefix(name, "lerd-php")
		short = strings.TrimSuffix(short, "-fpm.container")
		if len(short) < 2 {
			continue
		}
		// Convert "84" → "8.4"
		version := string(short[0]) + "." + short[1:]
		versions = append(versions, version)
	}
	return versions, nil
}

// EnsurePathMounted checks whether the given path is accessible inside the
// PHP-FPM and nginx containers. If the path is outside $HOME and not already
// volume-mounted, the quadlets are updated and containers restarted
// transparently before returning.
func EnsurePathMounted(path, phpVersion string) {
	home, _ := os.UserHomeDir()
	if home == "" {
		return
	}
	homePrefix := home
	if !strings.HasSuffix(homePrefix, "/") {
		homePrefix += "/"
	}
	if path == home || strings.HasPrefix(path, homePrefix) {
		return
	}

	versions, _ := listInstalledPHPVersions()

	// Collect all quadlet files to check: FPM containers + nginx.
	type quadletInfo struct {
		unitName string
		path     string
	}
	var quadlets []quadletInfo
	for _, v := range versions {
		short := strings.ReplaceAll(v, ".", "")
		unitName := "lerd-php" + short + "-fpm"
		quadlets = append(quadlets, quadletInfo{unitName, filepath.Join(config.QuadletDir(), unitName+".container")})
	}
	quadlets = append(quadlets, quadletInfo{"lerd-nginx", filepath.Join(config.QuadletDir(), "lerd-nginx.container")})

	var changedUnits []string
	for _, q := range quadlets {
		existing, readErr := os.ReadFile(q.path)
		if readErr != nil {
			continue
		}

		volumePrefix := fmt.Sprintf("Volume=%s:%s:", path, path)
		if strings.Contains(string(existing), volumePrefix) {
			continue
		}

		updated := InjectExtraVolumes(string(existing), []string{path})
		if updated == string(existing) {
			continue
		}
		if writeErr := os.WriteFile(q.path, []byte(updated), 0644); writeErr != nil {
			continue
		}
		changedUnits = append(changedUnits, q.unitName)
	}

	if len(changedUnits) > 0 {
		_ = DaemonReload()
		for _, unit := range changedUnits {
			_ = RestartUnit(unit)
		}
	}
}

// EnsureUserIni creates the per-version user php.ini with defaults if it doesn't exist.
// Same bind-mount race as EnsureXdebugIni: when this path is missing at FPM
// container start time, podman auto-creates it as a directory and the next
// EnsureUserIni call (which only Stat'd, didn't IsDir-check) silently no-ops
// while the user's php.ini is never written. Heal stale directories before
// returning the no-op fast path.
func EnsureUserIni(version string) error {
	path := config.PHPUserIniFile(version)
	if info, err := os.Stat(path); err == nil {
		if !info.IsDir() {
			return nil // already a regular file
		}
		if rmErr := os.Remove(path); rmErr != nil {
			return fmt.Errorf("removing stale user ini directory: %w", rmErr)
		}
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
