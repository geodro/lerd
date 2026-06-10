package podman

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

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

// fpmContainerfileHashLabel is stamped on every PHP-FPM image so
// NeedsFPMRebuild can detect drift even when the cache file lies (lerd
// < v1.22.0 advanced the cache without actually rebuilding).
const fpmContainerfileHashLabel = "dev.lerd.fpm.containerfile-hash"

// Seams for NeedsFPMRebuild so tests can fake the podman shell-outs.
var (
	imageLabelFn        = imageLabel
	containerfileHashFn = ContainerfileHash
)

// FPMImageName returns the local image tag for a PHP version, e.g.
// "lerd-php83-fpm:local" for "8.3". Centralised so callers and the
// rebuild-detection logic agree on the naming convention.
func FPMImageName(version string) string {
	return "lerd-php" + strings.ReplaceAll(version, ".", "") + "-fpm:local"
}

// NeedsFPMRebuild returns true when the embedded Containerfile differs
// from the cache file OR from any active version's image label (catches
// the pre-v1.22.0 poisoned-cache state). Scoped to activeVersions so
// orphaned legacy images for versions the user has since removed don't
// trigger a perpetual rebuild loop. False on a fresh install.
func NeedsFPMRebuild(activeVersions []string) bool {
	current, err := containerfileHashFn()
	if err != nil {
		// Hash unreadable means we cannot prove the image is current; force
		// a rebuild rather than silently treat it as up to date.
		return true
	}
	if stored, err := os.ReadFile(config.PHPImageHashFile()); err == nil {
		if strings.TrimSpace(string(stored)) != current {
			return true
		}
	}
	// Cache file says we're up to date; verify against the label on each
	// active version's image so a poisoned cache from older lerd binaries
	// still triggers a rebuild, while ignoring orphan legacy images.
	for _, v := range activeVersions {
		if imageLabelFn(FPMImageName(v), fpmContainerfileHashLabel) != current {
			return true
		}
	}
	return false
}

// imageLabel reads a single label from a local image. Returns "" on any
// error (image missing, podman unreachable, label absent) so callers
// treat that as "doesn't match" and fall back to a rebuild.
func imageLabel(image, key string) string {
	out, err := exec.Command(PodmanBin(), "inspect",
		"--format", "{{index .Config.Labels \""+key+"\"}}",
		image,
	).Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if v == "<no value>" {
		return ""
	}
	return v
}

// fpmBuildArgs returns the `podman build` flags shared by both build
// paths in buildFPMImage, before either appends the `-f <ctx>` tail.
// Extracted so the load-bearing `--label` arg has unit-test coverage.
func fpmBuildArgs(imageName, containerfileHash string, force bool) []string {
	args := []string{
		"build",
		"-t", imageName,
		"--label", fpmContainerfileHashLabel + "=" + containerfileHash,
	}
	if force {
		// Bypass layer cache so changes are fully applied. The old image
		// stays tagged and the container keeps running until we restart
		// the unit.
		args = append(args, "--no-cache")
	}
	return args
}

// fpmHashMu serializes StoreFPMHash so the per-version buildFPMImage calls
// fired in parallel by php:rebuild can't truncate-and-write each other.
var fpmHashMu sync.Mutex

// StoreFPMHash writes the current Containerfile hash to disk.
func StoreFPMHash() error {
	hash, err := ContainerfileHash()
	if err != nil {
		return err
	}
	fpmHashMu.Lock()
	defer fpmHashMu.Unlock()
	return os.WriteFile(config.PHPImageHashFile(), []byte(hash), 0644)
}

// BuildFPMImage builds the lerd PHP-FPM image for the given version if it doesn't exist.
// When local is false, it attempts to pull a pre-built base image from ghcr.io first.
func BuildFPMImage(version string, local bool) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	return buildFPMImage(version, false, local, cfg.GetExtensions(version), cfg.AllExtApkDeps(), cfg.GetPackages(version), os.Stdout)
}

// BuildFPMImageTo builds the PHP-FPM image writing output to w.
// When local is false, it attempts to pull a pre-built base image from ghcr.io first.
func BuildFPMImageTo(version string, local bool, w io.Writer) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	return buildFPMImage(version, false, local, cfg.GetExtensions(version), cfg.AllExtApkDeps(), cfg.GetPackages(version), w)
}

// RebuildFPMImage force-removes and rebuilds the PHP-FPM image for the given version.
// When local is false, it attempts to pull a pre-built base image from ghcr.io first.
func RebuildFPMImage(version string, local bool) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	return buildFPMImage(version, true, local, cfg.GetExtensions(version), cfg.AllExtApkDeps(), cfg.GetPackages(version), os.Stdout)
}

// RebuildFPMImageTo force-rebuilds the PHP-FPM image writing output to w.
// When local is false, it attempts to pull a pre-built base image from ghcr.io first.
func RebuildFPMImageTo(version string, local bool, w io.Writer) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	return buildFPMImage(version, true, local, cfg.GetExtensions(version), cfg.AllExtApkDeps(), cfg.GetPackages(version), w)
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
	base = strings.ReplaceAll(base, "{{.CustomExtensionsRuntime}}", "")
	base = strings.ReplaceAll(base, "{{.CustomPackages}}", "")
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
	args = append(args, PlatformPullArgs(ref)...)
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

func buildFPMImage(version string, force, local bool, customExts []string, extDeps map[string][]string, packages []string, w io.Writer) error {
	imageName := FPMImageName(version)

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

	// Stamp the Containerfile hash as an image label so NeedsFPMRebuild
	// can detect drift even when the on-disk cache file is stale (the
	// pre-v1.22.0 poisoning bug). Both build paths inherit the same args.
	canonicalHash, hashErr := ContainerfileHash()
	if hashErr != nil {
		return fmt.Errorf("computing Containerfile hash for label: %w", hashErr)
	}

	var containerfile string
	buildArgs := fpmBuildArgs(imageName, canonicalHash, force)

	// Fast path: pull pre-built base and layer just mkcert CA + custom extensions on top.
	if !local {
		if baseRef := tryPullBaseImage(version, w); baseRef != "" {
			containerfile = "FROM " + baseRef + "\n" +
				"RUN mkdir -p /etc/my.cnf.d && printf '[client]\\nssl=0\\n' > /etc/my.cnf.d/lerd-no-ssl.cnf\n" +
				buildCustomExtBlock(customExts, extDeps) +
				buildCustomPackagesBlock(packages) +
				mkcertCABlock(tmp)
			goto build
		}
	}

	// Slow path: full local build from the embedded Containerfile template.
	// The template compiles lerd_devtools in the builder stage via
	// `COPY internal/podman/devtools`, so stage that source into the build
	// context (the prebuilt base already carries it, so the fast path above
	// doesn't need it).
	{
		if err := writeDevtoolsSource(tmp); err != nil {
			return fmt.Errorf("staging devtools source: %w", err)
		}
		tmpl, tmplErr := GetQuadletTemplate("lerd-php-fpm.Containerfile")
		if tmplErr != nil {
			return tmplErr
		}
		containerfile = strings.ReplaceAll(tmpl, "{{.Version}}", version)
		containerfile = strings.ReplaceAll(containerfile, "{{.CustomExtensions}}", buildCustomExtBlock(customExts, extDeps))
		containerfile = strings.ReplaceAll(containerfile, "{{.CustomExtensionsRuntime}}", buildCustomExtRuntimeDeps(customExts, extDeps))
		containerfile = strings.ReplaceAll(containerfile, "{{.CustomPackages}}", buildCustomPackagesBlock(packages))
		containerfile = strings.ReplaceAll(containerfile, "{{.MkcertCA}}", mkcertCABlock(tmp))
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

	// Stamp the hash only after a real build — callers that no-op when the
	// image already exists must not advance the hash, otherwise a later
	// install would skip rebuilds for a template that never hit disk.
	if err := StoreFPMHash(); err != nil {
		fmt.Fprintf(w, "  WARN: storing PHP-FPM image hash: %v\n", err)
	}

	fmt.Fprintf(w, "  PHP %s image built successfully.\n", version)
	return nil
}

// extApkDeps maps a custom PHP extension to the Alpine packages its build needs.
// The standard bundle's -dev packages are already in the base image, so this only
// lists extensions whose build deps aren't there; without them PECL fails (e.g.
// imap's "U8T_CANONICAL is missing"). Users can add more via `lerd php:ext add
// --apk-deps`; the two sets are unioned. The "|| true" in the RUN block keeps a
// broken build from bricking later rebuilds, so VerifyExtensionLoaded checks the
// result afterward.
var extApkDeps = map[string][]string{
	"imap": {"imap-dev", "krb5-dev", "openssl-dev", "c-client"},
}

// validApkPkgName matches Alpine package names; used to reject anything that
// could break out of the `apk add` shell command in the generated Containerfile.
var validApkPkgName = regexp.MustCompile(`^[a-zA-Z0-9._+-]+$`)

// ParseApkDeps splits a space/comma/whitespace-separated package list and
// validates each name. Returns nil for empty input.
func ParseApkDeps(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(fields) == 0 {
		return nil, nil
	}
	deps := make([]string, 0, len(fields))
	for _, f := range fields {
		if !validApkPkgName.MatchString(f) {
			return nil, fmt.Errorf("invalid Alpine package name %q", f)
		}
		deps = append(deps, f)
	}
	return deps, nil
}

// apkDepsForExt returns the union of the built-in and user-configured Alpine
// packages for ext, deduplicated, in a stable order.
func apkDepsForExt(ext string, userDeps map[string][]string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(pkgs []string) {
		for _, p := range pkgs {
			p = strings.TrimSpace(p)
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	add(extApkDeps[ext])
	add(userDeps[ext])
	return out
}

// buildCustomExtRuntimeDeps emits an apk RUN line that reinstalls the
// builder-stage deps in the runtime stage so compiled .so files can
// dlopen against those system libs. Empty when no custom exts have deps.
func buildCustomExtRuntimeDeps(exts []string, userDeps map[string][]string) string {
	seen := map[string]bool{}
	var deps []string
	for _, ext := range exts {
		for _, pkg := range apkDepsForExt(ext, userDeps) {
			if seen[pkg] {
				continue
			}
			seen[pkg] = true
			deps = append(deps, pkg)
		}
	}
	if len(deps) == 0 {
		return ""
	}
	return "RUN apk add --no-cache " + strings.Join(deps, " ") + " && rm -rf /var/cache/apk/*\n"
}

// buildCustomExtBlock generates Dockerfile RUN blocks for user-configured
// extensions, apk-adding any extra build deps (built-in map ∪ userDeps) first.
func buildCustomExtBlock(exts []string, userDeps map[string][]string) string {
	if len(exts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("# User-configured extensions\n")
	for _, ext := range exts {
		prefix := ""
		if deps := apkDepsForExt(ext, userDeps); len(deps) > 0 {
			prefix = "apk add --no-cache " + strings.Join(deps, " ") + " && "
		}
		// `yes ''` feeds default answers to interactive PECL prompts (imap asks
		// for kerberos / c-client paths); harmless for extensions that don't ask.
		sb.WriteString(fmt.Sprintf(
			"RUN { %s(yes '' | pecl install %s && docker-php-ext-enable %s) || docker-php-ext-install %s || true; } \\\n    && rm -rf /tmp/pear /var/cache/apk/*\n",
			prefix, ext, ext, ext,
		))
	}
	return sb.String()
}

// buildCustomPackagesBlock emits an apk RUN line installing user-requested
// extra Alpine packages (lerd php:pkg) into the runtime stage, deduped and in a
// stable order. Names are validated so a bad entry can't break out of the apk
// command; invalid ones are dropped. Empty when there are no packages.
func buildCustomPackagesBlock(packages []string) string {
	seen := map[string]bool{}
	var valid []string
	for _, p := range packages {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] || !validApkPkgName.MatchString(p) {
			continue
		}
		seen[p] = true
		valid = append(valid, p)
	}
	if len(valid) == 0 {
		return ""
	}
	return "# User-requested extra packages (lerd php:pkg)\nRUN apk add --no-cache " +
		strings.Join(valid, " ") + " && rm -rf /var/cache/apk/*\n"
}

// phpExtensionLoaded reports whether ext appears in `php -m` output (case-insensitive).
func phpExtensionLoaded(moduleOutput, ext string) bool {
	want := strings.ToLower(strings.TrimSpace(ext))
	if want == "" {
		return false
	}
	for _, line := range strings.Split(moduleOutput, "\n") {
		if strings.ToLower(strings.TrimSpace(line)) == want {
			return true
		}
	}
	return false
}

// VerifyExtensionLoaded checks that the freshly built FPM image for the given
// version actually loads ext, by running `php -m` inside it. Returns an error if
// it isn't loaded (the PECL build failed and was swallowed by the "|| true" guard
// in the custom-extension RUN block).
func VerifyExtensionLoaded(version, ext string) error {
	imageName := FPMImageName(version)
	out, err := exec.Command(PodmanBin(), "run", "--rm", imageName, "php", "-m").CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspecting extensions in %s: %w\n%s", imageName, err, out)
	}
	if !phpExtensionLoaded(string(out), ext) {
		return fmt.Errorf("extension %q did not load in the rebuilt image (its build likely failed; check the extension name is correct, or pass --apk-deps with the Alpine packages it needs)", ext)
	}
	return nil
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
	if err := EnsureDumpAssets(); err != nil {
		return fmt.Errorf("ensuring dump assets: %w", err)
	}
	if err := EnsureProfilerAssets(); err != nil {
		return fmt.Errorf("ensuring profiler assets: %w", err)
	}
	if err := EnsureDevtoolsAssets(); err != nil {
		return fmt.Errorf("ensuring devtools assets: %w", err)
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
	content = strings.ReplaceAll(content, "{{.DumpsDir}}", config.DumpsAssetsDir())
	content = strings.ReplaceAll(content, "{{.DumpsIniPath}}", config.DumpsIniFile())
	content = strings.ReplaceAll(content, "{{.DevtoolsIniPath}}", config.DevtoolsIniFile())
	content = strings.ReplaceAll(content, "{{.SpxIniPath}}", config.SpxIniFile())
	content = strings.ReplaceAll(content, "{{.SpxDataDir}}", config.SpxDataDir())
	content = strings.ReplaceAll(content, "{{.HostNameLine}}", hostNameLine())
	content = applyShellMounts(content, short)
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

	if _, err := WriteQuadletDiff(unitName, content); err != nil {
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
		content = strings.ReplaceAll(content, "{{.DumpsDir}}", config.DumpsAssetsDir())
		content = strings.ReplaceAll(content, "{{.DumpsIniPath}}", config.DumpsIniFile())
		content = strings.ReplaceAll(content, "{{.DevtoolsIniPath}}", config.DevtoolsIniFile())
		content = strings.ReplaceAll(content, "{{.SpxIniPath}}", config.SpxIniFile())
		content = strings.ReplaceAll(content, "{{.SpxDataDir}}", config.SpxDataDir())
		content = strings.ReplaceAll(content, "{{.HostNameLine}}", hostNameLine())
		content = applyShellMounts(content, short)
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

// zshHistoryDir returns the per-PHP-version host directory that backs the
// container's /root/.zsh_state mount, creating it so the bind mount succeeds
// on first start. We deliberately do not mount any host shell config —
// see internal/podman/quadlets/lerd-php-fpm.Containerfile for the rationale.
func zshHistoryDir(versionShort string) string {
	dir := filepath.Join(config.DataDir(), "shell-state", "php-"+versionShort, "zsh")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// hostNameLine returns the `HostName=<host>` directive for the FPM quadlet so
// prompts inside the container read e.g. "root@laptop" instead of the
// auto-generated podman container id. Returns an empty string when the host
// hostname can't be read or contains characters podman would reject, so the
// placeholder line collapses cleanly.
func hostNameLine() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	for _, r := range h {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '.'
		if !ok {
			return ""
		}
	}
	return "HostName=" + h
}

// applyShellMounts substitutes shell-related template fields.
func applyShellMounts(content, versionShort string) string {
	return strings.ReplaceAll(content, "{{.ZshHistoryDir}}", zshHistoryDir(versionShort))
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

// ephemeralPathPrefixes lists filesystem trees that are system-managed and
// short-lived — there is no reason to bind-mount them into FPM/nginx, and
// IDEs (PhpStorm, VSCode) drop temp .php files into /tmp with random names
// that, mounted blindly, cascade FPM restarts every time the IDE invokes
// `php` on a fresh path.
var ephemeralPathPrefixes = []string{
	"/tmp/", "/var/tmp/",
	"/run/", "/proc/", "/sys/", "/dev/",
}

// pathMountAttempts memoises recent EnsurePathMounted calls so a runaway
// caller (IDE running `php` repeatedly with rotating temp paths, broken
// shell loop) cannot keep rewriting the FPM quadlet and re-triggering
// RestartUnit at the cadence required to hit systemd's start rate-limit.
var (
	pathMountAttemptsMu sync.Mutex
	pathMountAttempts   = map[string]time.Time{}
)

const pathMountDebounce = 60 * time.Second

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
	for _, p := range ephemeralPathPrefixes {
		if strings.HasPrefix(path, p) {
			return // ephemeral system dir, never bind-mount
		}
	}

	pathMountAttemptsMu.Lock()
	if last, ok := pathMountAttempts[path]; ok && time.Since(last) < pathMountDebounce {
		pathMountAttemptsMu.Unlock()
		return // already attempted recently; refuse to cascade restart again
	}
	pathMountAttempts[path] = time.Now()
	pathMountAttemptsMu.Unlock()

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
