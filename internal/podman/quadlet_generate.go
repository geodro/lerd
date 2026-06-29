package podman

import (
	"fmt"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// CustomServiceQuadletMarker tags quadlets generated for lerd-managed services
// (custom + default presets) so ReconcileServices finds orphans by content, not
// by name, sparing site/worker quadlets that lack it (issue #678).
const CustomServiceQuadletMarker = "# lerd-managed-service"

// GenerateCustomQuadlet builds a quadlet .container file for a custom service.
func GenerateCustomQuadlet(svc *config.CustomService) string {
	var b strings.Builder

	b.WriteString(CustomServiceQuadletMarker + "\n")
	b.WriteString("[Unit]\n")
	desc := svc.Description
	if desc == "" {
		desc = "Lerd " + svc.Name
	}
	fmt.Fprintf(&b, "Description=%s\n", desc)
	b.WriteString("After=network.target\n")

	b.WriteString("\n[Container]\n")
	fmt.Fprintf(&b, "Image=%s\n", svc.Image)
	fmt.Fprintf(&b, "ContainerName=lerd-%s\n", svc.Name)
	b.WriteString("Network=lerd\n")
	// Bound podman's graceful-stop window so images with slow shutdown
	// sequences (selenium/supervisord, chromium) don't block systemctl stop
	// for the full 90 s default. Mirrors the --stop-timeout=5 used on macOS.
	// StopTimeout= in [Container] requires Podman >=5.0; on Ubuntu 24.04's
	// 4.9.3 the key is unrecognised and quadlet aborts with exit 1, leaving
	// no service units at all (#299). Fall back to PodmanArgs= which works
	// on every quadlet-supporting podman.
	if supportsContainerStopTimeoutKey() {
		b.WriteString("StopTimeout=5\n")
	} else {
		b.WriteString("PodmanArgs=--stop-timeout=5\n")
	}

	// catatonit as PID 1 so SIGTERM reaches the main process. Without
	// this, mysqld in 8.4 ignores podman stop and restarts time out at
	// 90s (issue #380).
	if svc.Init {
		b.WriteString("PodmanArgs=--init\n")
	}

	if svc.ShareHosts {
		fmt.Fprintf(&b, "Volume=%s:/etc/hosts:ro,z\n", config.BrowserHostsFile())
	}

	for _, port := range svc.Ports {
		fmt.Fprintf(&b, "PublishPort=%s\n", port)
	}

	if svc.DataDir != "" {
		hostDir := config.DataSubDir(svc.Name)
		flags := "z"
		if svc.ChownData {
			flags += ",U"
		}
		fmt.Fprintf(&b, "Volume=%s:%s:%s\n", hostDir, svc.DataDir, flags)
	}

	if svc.Userns != "" {
		fmt.Fprintf(&b, "UserNS=%s\n", svc.Userns)
	}

	for _, f := range config.PresetFiles(svc.Preset) {
		hostPath := config.ServiceFilePath(svc.Name, f.Target)
		flags := "z"
		if f.Chown {
			flags += ",U"
		}
		fmt.Fprintf(&b, "Volume=%s:%s:%s\n", hostPath, f.Target, flags)
	}

	// User tuning override, mounted read-only after the bundled preset config so
	// the user's values win. Materialised by MaterializeServiceTuning, which runs
	// before generation, so the host path is guaranteed present for tunable
	// families.
	if target, ok := config.ServiceTuningMount(svc); ok {
		fmt.Fprintf(&b, "Volume=%s:%s:ro,z\n", config.ServiceTuningFile(svc.Name), target)
	}

	// Lerd-managed tuning helper (e.g. postgres config_file wrapper). Mounted
	// read-only; materialised by MaterializeServiceTuning before generation so
	// the host path is guaranteed present.
	if auxTarget, _, ok := config.ServiceTuningAux(svc); ok {
		fmt.Fprintf(&b, "Volume=%s:%s:ro,z\n", config.ServiceTuningAuxFile(svc.Name), auxTarget)
	}

	env := make(map[string]string, len(svc.Environment)+1)
	for k, v := range svc.Environment {
		env[k] = v
	}
	// Preset-sourced proxy env (e.g. redisinsight RI_PROXY_PATH) so the upstream
	// serves under the lerd-ui proxy mount. Overlaid here rather than stored in
	// the service YAML, so existing installs pick it up on the next start.
	if k, v, ok := config.PresetProxyEnv(svc); ok {
		env[k] = v
	}
	envKeys := make([]string, 0, len(env))
	for k := range env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		// systemd splits Environment= on whitespace and strips unescaped
		// double quotes, so JSON / quoted-wildcard values get mangled.
		// Wrap the whole pair and escape inner quotes to preserve them.
		escaped := strings.ReplaceAll(env[k], `"`, `\"`)
		fmt.Fprintf(&b, "Environment=\"%s=%s\"\n", k, escaped)
	}

	// Prefer the service's own Exec; otherwise, if its family needs a command to
	// read the tuning override (redis/postgres), use that. The mount itself was
	// emitted above; mysql/mariadb auto-include their conf dir and need no command.
	exec := svc.Exec
	if exec == "" {
		if cmd, ok := config.ServiceTuningCommand(svc); ok {
			exec = cmd
		}
	}
	if exec != "" {
		fmt.Fprintf(&b, "Exec=%s\n", exec)
	}

	b.WriteString("\n[Service]\n")
	b.WriteString("Restart=always\n")

	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=default.target\n")

	return b.String()
}
