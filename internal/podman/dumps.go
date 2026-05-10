package podman

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

//go:embed dumpbridge
var dumpBridgeFS embed.FS

// In-container mount targets for the bridge assets. The bridge file lives
// outside conf.d so it isn't auto-loaded as a php.ini; the conf.d ini is the
// only thing that activates it (auto_prepend_file=...).
const (
	containerDumpBridgePath = "/usr/local/etc/lerd/dump-bridge.php"
	containerDumpIniPath    = "/usr/local/etc/php/conf.d/97-lerd-dump.ini"
)

// DumpBridgePHP returns the embedded contents of dump-bridge.php as a string.
// Exposed for tests so they can assert the on-disk file is byte-identical to
// the embed without re-reading the embed FS.
func DumpBridgePHP() (string, error) {
	b, err := dumpBridgeFS.ReadFile("dumpbridge/dump-bridge.php")
	if err != nil {
		return "", fmt.Errorf("dump bridge embed: %w", err)
	}
	return string(b), nil
}

// DumpBridgeIni returns the conf.d ini content with the {{ DUMP_TARGET }}
// placeholder substituted for the local lerd-ui dump socket path. The
// socket is bind-mounted into every FPM container via the standard %h:%h
// volume, so containers reach it at the same host path.
func DumpBridgeIni() (string, error) {
	b, err := dumpBridgeFS.ReadFile("dumpbridge/97-lerd-dump.ini")
	if err != nil {
		return "", fmt.Errorf("dump bridge ini embed: %w", err)
	}
	target := "unix://" + config.DumpsSocketPath()
	return strings.ReplaceAll(string(b), "{{ DUMP_TARGET }}", target), nil
}

// WriteDumpBridgeAssets writes the bridge PHP file and the conf.d ini to
// their host paths under DataDir()/php/dumps/. Idempotent: a regular file
// whose contents already match the embed is left untouched. Replaces a
// directory at the same path that podman might have auto-created earlier.
func WriteDumpBridgeAssets() error {
	dir := config.DumpsAssetsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating dumps dir: %w", err)
	}

	phpContent, err := DumpBridgePHP()
	if err != nil {
		return err
	}
	iniContent, err := DumpBridgeIni()
	if err != nil {
		return err
	}

	for _, asset := range []struct {
		path    string
		content string
	}{
		{config.DumpsBridgeFile(), phpContent},
		{config.DumpsIniFile(), iniContent},
	} {
		if info, err := os.Stat(asset.path); err == nil {
			if info.IsDir() {
				if rmErr := os.RemoveAll(asset.path); rmErr != nil {
					return fmt.Errorf("removing stale dump asset directory %s: %w", asset.path, rmErr)
				}
			} else if existing, readErr := os.ReadFile(asset.path); readErr == nil && string(existing) == asset.content {
				continue
			}
		}
		if err := os.WriteFile(asset.path, []byte(asset.content), 0644); err != nil {
			return fmt.Errorf("writing dump asset %s: %w", asset.path, err)
		}
	}
	return nil
}

// RemoveDumpAssets deletes the host-side bridge file and ini. Safe to call
// when the assets are already absent (the missing-file errors are swallowed).
// The caller is responsible for rewriting FPM quadlets so the (now stale)
// Volume= lines disappear.
func RemoveDumpAssets() error {
	for _, p := range []string{config.DumpsBridgeFile(), config.DumpsIniFile()} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", p, err)
		}
	}
	// Clean up the empty directory if nothing else lives in it.
	dir := config.DumpsAssetsDir()
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
	return nil
}

// dumpVolumeLines builds the two FPM Volume= lines that bind-mount the bridge
// assets read-only into the container. Returned with a trailing newline so
// callers can drop the result straight into the quadlet template.
func dumpVolumeLines() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Volume=%s:%s:ro\n", config.DumpsBridgeFile(), containerDumpBridgePath)
	fmt.Fprintf(&sb, "Volume=%s:%s:ro\n", config.DumpsIniFile(), containerDumpIniPath)
	return sb.String()
}

// ApplyDumpVolumes splices the two dump-bridge Volume= lines into a rendered
// FPM container quadlet when enabled. When disabled, any previously injected
// lines are stripped so a freshly written quadlet matches the embedded
// template byte-for-byte and WriteQuadletDiff sees a clean diff.
//
// Insertion is keyed off the existing 98-lerd-user.ini Volume line so we
// never end up wedging the bridge above /etc/hosts (which would change
// container startup ordering for no reason). When the anchor line isn't
// present (e.g. tests calling with a stripped template), insertion is a
// no-op: the caller is expected to handle that as misconfiguration upstream.
func ApplyDumpVolumes(content string, enabled bool) string {
	stripped := stripDumpVolumes(content)
	if !enabled {
		return stripped
	}
	lines := strings.Split(stripped, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "98-lerd-user.ini:ro") {
			continue
		}
		insert := strings.TrimRight(dumpVolumeLines(), "\n")
		out := make([]string, 0, len(lines)+2)
		out = append(out, lines[:i+1]...)
		out = append(out, strings.Split(insert, "\n")...)
		out = append(out, lines[i+1:]...)
		return strings.Join(out, "\n")
	}
	return stripped
}

// stripDumpVolumes removes any Volume= line that targets the bridge mount
// points, regardless of host path. Keeps ApplyDumpVolumes idempotent across
// repeated calls and lets us migrate the host path later without orphan
// lines piling up in old quadlets.
func stripDumpVolumes(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Volume=") &&
			(strings.Contains(trimmed, containerDumpBridgePath) || strings.Contains(trimmed, containerDumpIniPath)) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// EnsureDumpAssets is the partner of EnsureXdebugIni. It guarantees the
// bridge and ini exist as regular files when Dumps.Enabled is true, so podman
// doesn't auto-create directories at the bind-mount source paths if a fresh
// install hasn't run `lerd dump on` yet.
func EnsureDumpAssets() error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	if !cfg.IsDumpsEnabled() {
		return nil
	}
	for _, p := range []string{config.DumpsBridgeFile(), config.DumpsIniFile()} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			continue
		} else if err == nil && info.IsDir() {
			if rmErr := os.RemoveAll(p); rmErr != nil {
				return fmt.Errorf("removing stale dump asset directory %s: %w", p, rmErr)
			}
		}
		// Missing or just removed: write a fresh copy.
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return err
		}
	}
	return WriteDumpBridgeAssets()
}
