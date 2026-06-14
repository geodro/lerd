package podman

import (
	"fmt"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// FrankenPHPContainerName returns the Podman container name for a site's
// FrankenPHP container, e.g. "lerd-fp-myapp".
func FrankenPHPContainerName(siteName string) string {
	return "lerd-fp-" + siteName
}

// FrankenPHPImage returns the lerd-derived FrankenPHP image tag for the
// requested PHP version, e.g. "localhost/lerd-frankenphp84:local". This is the
// image the per-site quadlet runs: the dunglas base with lerd's standard
// extension set baked in (see BuildFrankenPHPImage). Versions without a
// published frankenphp base fall back to the latest one lerd knows about.
func FrankenPHPImage(phpVersion string) string {
	return FrankenPHPImageName(config.NormalizeFrankenPHPVersion(phpVersion))
}

// FrankenPHPBaseImage returns the upstream dunglas/frankenphp image tag the
// derived image is built FROM, for the requested PHP version.
func FrankenPHPBaseImage(phpVersion string) string {
	return "docker.io/dunglas/frankenphp:php" + config.NormalizeFrankenPHPVersion(phpVersion) + "-alpine"
}

// FrankenPHPPort is the port FrankenPHP listens on inside the container. Kept
// fixed so the nginx proxy target and framework entrypoints stay in sync.
const FrankenPHPPort = 8000

// GenerateFrankenPHPQuadlet builds a quadlet .container file for a per-site
// FrankenPHP container. The container mounts the project at its host path,
// joins the lerd network, and runs the framework's declared entrypoint. Any
// env map entries are written as Environment= lines.
func GenerateFrankenPHPQuadlet(siteName, projectPath, phpVersion string, entrypoint []string, env map[string]string) string {
	containerName := FrankenPHPContainerName(siteName)
	image := FrankenPHPImage(phpVersion)

	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=Lerd FrankenPHP container (%s)\n", siteName)
	b.WriteString("After=network.target\n")

	b.WriteString("\n[Container]\n")
	fmt.Fprintf(&b, "Image=%s\n", image)
	fmt.Fprintf(&b, "ContainerName=%s\n", containerName)
	b.WriteString("Network=lerd\n")
	fmt.Fprintf(&b, "Volume=%s:/etc/hosts:ro,z\n", config.ContainerHostsFile())
	fmt.Fprintf(&b, "Volume=%s:%s:rw\n", projectPath, projectPath)
	// Debug tooling: bind-mount the same conf.d inis, bridge assets, and runtime
	// socket dir the FPM container gets, so dump()/dd(), the Debug window
	// (lerd_devtools), and Xdebug work for requests Octane serves from this
	// container too. The baked extensions stay inert until these inis/sentinels
	// arm them. RunDir carries the unix socket the bridges ship to; it must appear
	// at its host path, matching the dump_host ini value. SPX is omitted: it can't
	// profile Octane's resident-worker requests (see the Containerfile).
	fmt.Fprintf(&b, "Volume=%s:/usr/local/etc/lerd:ro\n", config.DumpsAssetsDir())
	fmt.Fprintf(&b, "Volume=%s:/usr/local/etc/php/conf.d/97-lerd-dump.ini:ro\n", config.DumpsIniFile())
	fmt.Fprintf(&b, "Volume=%s:/usr/local/etc/php/conf.d/96-lerd-devtools.ini:ro\n", config.DevtoolsIniFile())
	fmt.Fprintf(&b, "Volume=%s:/usr/local/etc/php/conf.d/99-xdebug.ini:ro\n", config.PHPConfFile(phpVersion))
	// Per-site user php.ini override, edited from the site's config modal. Scoped
	// to this site (not the shared per-version file), since a FrankenPHP site runs
	// its own container.
	fmt.Fprintf(&b, "Volume=%s:/usr/local/etc/php/conf.d/98-lerd-user.ini:ro\n", config.SitePHPUserIniFile(siteName))
	fmt.Fprintf(&b, "Volume=%s:%s:rw\n", config.RunDir(), config.RunDir())
	fmt.Fprintf(&b, "PodmanArgs=--security-opt=label=disable --workdir=%s\n", projectPath)
	for _, k := range sortedKeys(env) {
		// systemd Environment= splits on whitespace unless the whole
		// KEY=value pair is quoted, so always wrap in double quotes.
		v := strings.ReplaceAll(env[k], `"`, `\"`)
		fmt.Fprintf(&b, "Environment=\"%s=%s\"\n", k, v)
	}
	if len(entrypoint) > 0 {
		fmt.Fprintf(&b, "Exec=%s\n", shellJoin(entrypoint))
	}

	b.WriteString("\n[Service]\n")
	b.WriteString("Restart=always\n")

	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=default.target\n")

	return b.String()
}

// RestartSiteContainersForVersion restarts every per-site PHP container on the
// given PHP version — custom-FPM and FrankenPHP — so a per-version ini change
// (php.ini, xdebug) reaches them too. Paused/ignored sites are skipped; the
// shared FPM container is restarted separately by the caller.
func RestartSiteContainersForVersion(version string) {
	reg, err := config.LoadSites()
	if err != nil {
		return
	}
	for _, s := range reg.Sites {
		if s.Paused || s.Ignored || s.PHPVersion != version {
			continue
		}
		switch {
		case s.IsCustomFPM():
			_ = RestartUnit(CustomFPMContainerName(s.Name))
		case s.IsFrankenPHP():
			_ = RestartUnit(FrankenPHPContainerName(s.Name))
		}
	}
}

// WriteFrankenPHPQuadlet writes the quadlet for a FrankenPHP site.
func WriteFrankenPHPQuadlet(siteName, projectPath, phpVersion string, entrypoint []string, env map[string]string) error {
	_, err := WriteFrankenPHPQuadletDiff(siteName, projectPath, phpVersion, entrypoint, env)
	return err
}

// WriteFrankenPHPQuadletDiff writes the quadlet and returns whether the content
// changed on disk so callers can decide whether to restart the running
// container. It first ensures every bind-mount source exists as a regular file,
// mirroring WriteFPMQuadlet: without this, a restart through any path other than
// the full link (a php.ini save, an install-time refresh) could mount a missing
// source, letting podman auto-create a directory at a conf.d/*.ini path and
// break the container's PHP startup.
func WriteFrankenPHPQuadletDiff(siteName, projectPath, phpVersion string, entrypoint []string, env map[string]string) (bool, error) {
	_ = EnsureSitePHPUserIni(siteName)
	_ = EnsureXdebugIni(phpVersion)
	_ = EnsureDumpAssets()
	_ = EnsureDevtoolsAssets()
	content := GenerateFrankenPHPQuadlet(siteName, projectPath, phpVersion, entrypoint, env)
	return WriteQuadletDiff(FrankenPHPContainerName(siteName), content)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// RemoveFrankenPHPQuadlet removes the unit file for a FrankenPHP site.
// RemoveQuadlet drops the launchd plist on macOS too via RemoveContainerUnitFn.
func RemoveFrankenPHPQuadlet(siteName string) error {
	return RemoveQuadlet(FrankenPHPContainerName(siteName))
}

// shellJoin quotes each argument for embedding in a quadlet Exec= line.
// Quadlet Exec values are passed through podman's argv parser which already
// handles single-word args; anything with whitespace needs quoting.
func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\"'\\") {
			out[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}
