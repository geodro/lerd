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

// BindForLAN rewrites every PublishPort= line in a quadlet so the host-side
// bind matches the requested LAN-exposure state. The embedded quadlet files
// use the unprefixed `PublishPort=80:80` form, which podman interprets as
// binding 0.0.0.0 (all interfaces). When lanExposed is false (the default
// safe-on-coffee-shop-wifi state) we rewrite each unprefixed line to
// `PublishPort=127.0.0.1:80:80` so only the local host can connect; when
// true, we leave the unprefixed form alone so LAN clients can reach the
// service.
//
// Lines that already have an explicit IP prefix (lerd-dns binds 127.0.0.1
// directly because the LAN path goes through the userspace forwarder, not
// the publish) are left untouched in both states.
func BindForLAN(content string, lanExposed bool) string {
	if lanExposed {
		// Already in the LAN-exposed form. Strip any explicit 127.0.0.1
		// prefix we may have written previously, EXCEPT for entries that
		// were originally pinned in the embed file (lerd-dns).
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "PublishPort=127.0.0.1:") {
				continue
			}
			// Preserve lerd-dns: it's the only quadlet that ships with an
			// explicit 127.0.0.1 prefix in the embed, because LAN access
			// to DNS is routed through the userspace forwarder rather
			// than a publish change.
			if strings.Contains(trimmed, ":5300:5300") {
				continue
			}
			rest := strings.TrimPrefix(trimmed, "PublishPort=127.0.0.1:")
			lines[i] = "PublishPort=" + rest
		}
		return strings.Join(lines, "\n")
	}

	// Not exposed: prefix every unprefixed PublishPort= with 127.0.0.1.
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "PublishPort=") {
			continue
		}
		value := strings.TrimPrefix(trimmed, "PublishPort=")
		// Skip lines that already have an explicit IP prefix (any host
		// IP, not just 127.0.0.1 — be conservative and leave operator
		// overrides alone). Detect by checking if the first segment
		// contains a dot (e.g. 127.0.0.1, 192.168.x.y) rather than being
		// a bare port number.
		firstSeg := strings.SplitN(value, ":", 2)[0]
		if strings.ContainsRune(firstSeg, '.') {
			continue
		}
		lines[i] = "PublishPort=127.0.0.1:" + value
	}
	return strings.Join(lines, "\n")
}
