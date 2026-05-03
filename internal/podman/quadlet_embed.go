package podman

import (
	"embed"
	"fmt"
	"os/exec"
	"strings"
)

//go:embed quadlets
var quadletFS embed.FS

// GetQuadletTemplate returns the content of a named quadlet template file.
func GetQuadletTemplate(name string) (string, error) {
	data, err := quadletFS.ReadFile("quadlets/" + name)
	if err != nil {
		return "", fmt.Errorf("quadlet template %q not found: %w", name, err)
	}
	return string(data), nil
}

// ApplyImage replaces the Image= line in quadlet content with the given image.
// If content contains no Image= line it is returned unchanged.
func ApplyImage(content, image string) string {
	if image == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Image=") {
			lines[i] = "Image=" + image
			return strings.Join(lines, "\n")
		}
	}
	return content
}

// CurrentImage returns the value of the Image= line in quadlet content,
// or "" if no such line exists.
func CurrentImage(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Image=") {
			return strings.TrimPrefix(trimmed, "Image=")
		}
	}
	return ""
}

// ApplyExtraPorts appends extra PublishPort lines to quadlet content.
func ApplyExtraPorts(content string, extraPorts []string) string {
	var sb strings.Builder
	sb.WriteString(content)
	for _, p := range extraPorts {
		fmt.Fprintf(&sb, "PublishPort=%s\n", p)
	}
	return sb.String()
}

// StripInstallSection removes the [Install] section from a quadlet's content
// when autostartDisabled is true, and returns the input unchanged when false.
//
// Quadlets are special: a `[Install] WantedBy=default.target` clause causes
// the podman-system-generator to create a symlink in
// `/run/user/$UID/systemd/generator/default.target.wants/` on every
// daemon-reload, which makes the unit auto-start at login regardless of
// `systemctl --user enable/disable` (those don't apply to generator units).
// The only way to actually stop a quadlet from auto-starting is to drop the
// [Install] section from the source .container file before the generator
// sees it. WriteQuadletDiff calls this centrally so every code path that
// writes a quadlet (install, services, MCP server, custom-service generator)
// honours the global autostart setting without each having to remember.
func StripInstallSection(content string, autostartDisabled bool) string {
	if !autostartDisabled {
		return content
	}
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	inInstall := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inInstall = trimmed == "[Install]"
			if inInstall {
				continue
			}
		}
		if inInstall {
			continue
		}
		out = append(out, line)
	}
	// Trim a trailing run of blank lines that would otherwise be left
	// behind when [Install] was the last section in the file.
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	out = append(out, "")
	return strings.Join(out, "\n")
}

// InjectPodmanArgs adds `PodmanArgs=<arg>` to the [Container] section.
// Idempotent: if any PodmanArgs= line already carries the same arg we
// return unchanged so the quadlet diff doesn't oscillate across writes.
func InjectPodmanArgs(content, arg string) string {
	if arg == "" {
		return content
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "PodmanArgs=") {
			continue
		}
		for _, f := range strings.Fields(strings.TrimPrefix(trimmed, "PodmanArgs=")) {
			if f == arg {
				return content
			}
		}
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Image=") {
			out := make([]string, 0, len(lines)+1)
			out = append(out, lines[:i+1]...)
			out = append(out, "PodmanArgs="+arg)
			out = append(out, lines[i+1:]...)
			return strings.Join(out, "\n")
		}
	}
	return content
}

// InjectExtraVolumes adds Volume= lines for paths that are not already covered
// by the %h:%h mount. Each path is bind-mounted read-write at the same location
// inside the container. Existing Volume= lines for the same host path are not
// duplicated.
func InjectExtraVolumes(content string, paths []string) string {
	if len(paths) == 0 {
		return content
	}
	var extra []string
	for _, p := range paths {
		// Check if this path is already mounted (with any flags).
		prefix := fmt.Sprintf("Volume=%s:%s:", p, p)
		if strings.Contains(content, prefix) {
			continue
		}
		extra = append(extra, fmt.Sprintf("Volume=%s:%s:rw", p, p))
	}
	if len(extra) == 0 {
		return content
	}
	// Insert after the Volume=%h:%h line (matches both :rw and :ro).
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Volume=%h:%h:") {
			out := make([]string, 0, len(lines)+len(extra))
			out = append(out, lines[:i+1]...)
			out = append(out, extra...)
			out = append(out, lines[i+1:]...)
			return strings.Join(out, "\n")
		}
	}
	return content
}

// OCIRuntime returns the name of the OCI runtime podman is currently configured to use.
func OCIRuntime() string {
	out, err := exec.Command(PodmanBin(), "info", "--format", "{{.Host.OCIRuntime.Name}}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// BindForLAN flips every PublishPort= between the loopback and LAN form on
// both stacks in lockstep: 127.0.0.1 ↔ bare and [::1] ↔ [::]. lerd-dns
// (:5300) is pinned on 127.0.0.1 in the embed because LAN access routes
// via the userspace forwarder, so its lines are preserved as-is.
func BindForLAN(content string, lanExposed bool) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "PublishPort=") {
			continue
		}
		// Preserve lerd-dns (pinned to 127.0.0.1 in the embed because LAN
		// DNS is routed via the userspace forwarder, not the publish).
		if strings.Contains(trimmed, ":5300:5300") {
			continue
		}
		value := strings.TrimPrefix(trimmed, "PublishPort=")

		if lanExposed {
			if rest, ok := strings.CutPrefix(value, "127.0.0.1:"); ok {
				lines[i] = "PublishPort=" + rest
			} else if rest, ok := strings.CutPrefix(value, "[::1]:"); ok {
				lines[i] = "PublishPort=[::]:" + rest
			}
			continue
		}

		if rest, ok := strings.CutPrefix(value, "[::]:"); ok {
			lines[i] = "PublishPort=[::1]:" + rest
			continue
		}
		if strings.HasPrefix(value, "[") {
			continue
		}
		firstSeg := strings.SplitN(value, ":", 2)[0]
		if strings.ContainsRune(firstSeg, '.') {
			continue
		}
		lines[i] = "PublishPort=127.0.0.1:" + value
	}
	return strings.Join(lines, "\n")
}

// PairIPv6Binds normalises PublishPort lines for dual-stack reach:
// 127.0.0.1:X gets paired with [::1]:X, bare/0.0.0.0:X is rewritten
// to [::]:X. Idempotent; skipped when Network= is absent (pasta path).
func PairIPv6Binds(content string) string {
	hasNetwork := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Network=") {
			hasNetwork = true
			break
		}
	}
	if !hasNetwork {
		return content
	}
	lines := strings.Split(content, "\n")

	v6PortSpecs := map[string]bool{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "PublishPort=") {
			continue
		}
		value := strings.TrimPrefix(trimmed, "PublishPort=")
		if !strings.HasPrefix(value, "[") {
			continue
		}
		end := strings.Index(value, "]")
		if end < 0 || end+1 >= len(value) || value[end+1] != ':' {
			continue
		}
		v6PortSpecs[value[end+2:]] = true
	}

	out := make([]string, 0, len(lines)*2)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "PublishPort=") {
			out = append(out, line)
			continue
		}
		value := strings.TrimPrefix(trimmed, "PublishPort=")
		if strings.HasPrefix(value, "[") {
			out = append(out, line)
			continue
		}

		firstSeg := strings.SplitN(value, ":", 2)[0]
		switch {
		case !strings.ContainsRune(firstSeg, '.'), firstSeg == "0.0.0.0":
			portSpec := strings.TrimPrefix(value, "0.0.0.0:")
			if v6PortSpecs[portSpec] {
				continue
			}
			v6PortSpecs[portSpec] = true
			out = append(out, "PublishPort=[::]:"+portSpec)
		case firstSeg == "127.0.0.1":
			out = append(out, line)
			portSpec := strings.TrimPrefix(value, "127.0.0.1:")
			if v6PortSpecs[portSpec] {
				continue
			}
			v6PortSpecs[portSpec] = true
			out = append(out, "PublishPort=[::1]:"+portSpec)
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
