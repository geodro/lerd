package podman

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// PodmanBin returns the full path to the podman binary. On macOS it searches
// well-known Homebrew locations when PATH is restricted (e.g. launchd services).
func PodmanBin() string {
	if p, err := exec.LookPath("podman"); err == nil {
		return p
	}
	for _, candidate := range []string{
		"/opt/homebrew/bin/podman",
		"/usr/local/bin/podman",
	} {
		if _, err := exec.Command(candidate, "--version").Output(); err == nil {
			return candidate
		}
	}
	return "podman"
}

// Cmd returns an exec.Cmd for podman with the given arguments, using PodmanBin()
// so the binary is found even under launchd's restricted PATH.
func Cmd(args ...string) *exec.Cmd {
	return exec.Command(PodmanBin(), args...)
}

// Run executes podman with the given arguments and returns stdout.
func Run(args ...string) (string, error) {
	cmd := exec.Command(PodmanBin(), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("podman %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RunSilent executes podman with the given arguments, discarding output.
func RunSilent(args ...string) error {
	_, err := Run(args...)
	return err
}

// ImageExists returns true if the named image is present in the local store.
func ImageExists(image string) bool {
	return RunSilent("image", "exists", image) == nil
}

// LocalImageDigest returns every known digest for the locally-stored image,
// in priority order: the platform-specific manifest digest first, then any
// repo digests recorded at pull time (which include the upstream manifest
// list digest used by Docker Hub for multi-arch images). Returns nil when
// the image isn't present locally.
func LocalImageDigest(image string) []string {
	out, err := Run("image", "inspect", image, "--format", "{{.Digest}}|{{range .RepoDigests}}{{.}} {{end}}")
	if err != nil {
		return nil
	}
	parts := strings.SplitN(strings.TrimSpace(out), "|", 2)
	digests := []string{}
	if len(parts) > 0 && parts[0] != "" {
		digests = append(digests, parts[0])
	}
	if len(parts) > 1 {
		for _, repo := range strings.Fields(parts[1]) {
			if at := strings.Index(repo, "@"); at >= 0 {
				digests = append(digests, repo[at+1:])
			}
		}
	}
	return digests
}

// PullImageTo pulls the named image, writing progress output to w.
func PullImageTo(image string, w io.Writer) error {
	cmd := exec.Command(PodmanBin(), "pull", image)
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pulling %s: %w", image, err)
	}
	return nil
}

// PullImageWithProgress pulls the named image and invokes onLine for each
// complete line of podman's output so callers can forward live progress.
func PullImageWithProgress(image string, onLine func(string)) error {
	if onLine == nil {
		return PullImageIfMissing(image)
	}
	cmd := exec.Command(PodmanBin(), "pull", image)
	w := &lineWriter{onLine: onLine}
	cmd.Stdout = w
	cmd.Stderr = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pulling %s: %w", image, err)
	}
	w.flush()
	return nil
}

// lineWriter invokes onLine per full line. Both \n and \r terminate, so
// podman's TTY progress-bar rewrites still surface as discrete events.
type lineWriter struct {
	buf    []byte
	onLine func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r\n")
		w.buf = w.buf[i+1:]
		if line != "" {
			w.onLine(line)
		}
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	line := strings.TrimRight(string(w.buf), "\r\n")
	w.buf = w.buf[:0]
	if line != "" {
		w.onLine(line)
	}
}

// PullImageIfMissing pulls the named image when it is not already in the
// local store. No-op when the image exists.
func PullImageIfMissing(image string) error {
	if ImageExists(image) {
		return nil
	}
	cmd := exec.Command(PodmanBin(), "pull", image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pulling %s: %w", image, err)
	}
	return nil
}

// ServiceImage returns the OCI image name embedded in a named quadlet template.
// Returns "" if the quadlet or Image line is not found.
func ServiceImage(quadletName string) string {
	content, err := GetQuadletTemplate(quadletName + ".container")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "Image=") {
			return strings.TrimPrefix(line, "Image=")
		}
	}
	return ""
}

// ServiceVersion extracts the major version from a built-in service's image tag.
// For example: mysql:8.0 → "8.0", postgis/postgis:16-3.5-alpine → "16",
// redis:7-alpine → "7", meilisearch:v1.7 → "1.7".
// Returns "" if the version cannot be determined.
func ServiceVersion(quadletName string) string {
	image := ServiceImage(quadletName)
	if image == "" {
		return ""
	}
	// Extract tag after the last colon.
	idx := strings.LastIndex(image, ":")
	if idx < 0 {
		return ""
	}
	tag := image[idx+1:]
	// Strip leading "v" prefix.
	tag = strings.TrimPrefix(tag, "v")
	// Take only the version part (before any dash-separated suffix like "-alpine").
	if dash := strings.Index(tag, "-"); dash > 0 {
		// Keep if it looks like a version (e.g. "8.0"), drop suffix like "-alpine".
		candidate := tag[:dash]
		if len(candidate) > 0 && candidate[0] >= '0' && candidate[0] <= '9' {
			return candidate
		}
	}
	// Return as-is if it starts with a digit.
	if len(tag) > 0 && tag[0] >= '0' && tag[0] <= '9' {
		return tag
	}
	return ""
}

// InstalledImage returns the Image= from the installed quadlet on disk,
// or falls back to the embedded template's Image=. Returns "" when neither
// exists. The installed file reflects user image overrides (global config,
// platform overrides) so display surfaces use this instead of ServiceImage.
func InstalledImage(unit string) string {
	path := filepath.Join(config.QuadletDir(), unit+".container")
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if after, ok := strings.CutPrefix(line, "Image="); ok {
				return strings.TrimSpace(after)
			}
		}
	}
	return ServiceImage(unit)
}

// ServiceVersionLabel returns a human-readable version label from an OCI
// image reference. Rolling tags like "latest" pass through unchanged; numeric
// tags are prefixed with "v" and stripped of distro/variant suffixes
// (mysql:8.0 → v8.0, redis:7-alpine → v7, postgis:16-3.5 → v16).
func ServiceVersionLabel(image string) string {
	if image == "" {
		return ""
	}
	idx := strings.LastIndex(image, ":")
	if idx < 0 {
		return ""
	}
	tag := image[idx+1:]
	switch tag {
	case "latest", "main", "master", "edge", "stable", "nightly":
		return tag
	}
	core := strings.TrimPrefix(tag, "v")
	if dash := strings.Index(core, "-"); dash > 0 {
		core = core[:dash]
	}
	if core == "" || core[0] < '0' || core[0] > '9' {
		return tag
	}
	return "v" + core
}

// ContainerRunning returns true if the named container is running.
func ContainerRunning(name string) (bool, error) {
	out, err := Run("inspect", "--format={{.State.Running}}", name)
	if err != nil {
		// container doesn't exist
		return false, nil
	}
	return strings.TrimSpace(out) == "true", nil
}

// ContainerExists returns true if the named container exists (running or not).
func ContainerExists(name string) (bool, error) {
	_, err := Run("inspect", "--format={{.Name}}", name)
	if err != nil {
		return false, nil
	}
	return true, nil
}
