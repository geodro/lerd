//go:build darwin

package services

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func init() {
	Mgr = &darwinServiceManager{}
}

type darwinServiceManager struct{}

// --- Path helpers ---

func launchAgentsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents")
}

func lerdLogsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Logs", "lerd")
}

func plistPath(name string) string {
	return filepath.Join(launchAgentsDir(), name+".plist")
}

func plistLabel(name string) string {
	return "com.lerd." + name
}

// --- Plist generation ---

func xmlEscStr(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s)) //nolint:errcheck
	return buf.String()
}

func buildPlist(lbl string, args []string, runAtLoad, keepAlive bool, logPath string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>`)
	sb.WriteString(xmlEscStr(lbl))
	sb.WriteString("</string>\n\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range args {
		sb.WriteString("\t\t<string>")
		sb.WriteString(xmlEscStr(a))
		sb.WriteString("</string>\n")
	}
	sb.WriteString("\t</array>\n")
	if runAtLoad {
		sb.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	}
	if keepAlive {
		sb.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	}
	if logPath != "" {
		sb.WriteString("\t<key>StandardOutPath</key>\n\t<string>")
		sb.WriteString(xmlEscStr(logPath))
		sb.WriteString("</string>\n\t<key>StandardErrorPath</key>\n\t<string>")
		sb.WriteString(xmlEscStr(logPath))
		sb.WriteString("</string>\n")
	}
	sb.WriteString("</dict>\n</plist>\n")
	return sb.String()
}

func ensurePlistDirs(name string) error {
	if err := os.MkdirAll(launchAgentsDir(), 0755); err != nil {
		return err
	}
	return os.MkdirAll(lerdLogsDir(), 0755)
}

// --- INI / Quadlet parser ---

// parseSection returns key → []values for a named INI section in content.
func parseSection(content, section string) map[string][]string {
	result := map[string][]string{}
	inSection := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSection = line[1:len(line)-1] == section
			continue
		}
		if !inSection {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx >= 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			result[key] = append(result[key], val)
		}
	}
	return result
}

// expandSpecifiers replaces Quadlet path specifiers (%h → home dir).
func expandSpecifiers(s string) string {
	home, _ := os.UserHomeDir()
	return strings.ReplaceAll(s, "%h", home)
}

// podmanBinPath returns the path to the podman binary.
func podmanBinPath() string {
	if p, err := exec.LookPath("podman"); err == nil {
		return p
	}
	// Homebrew default locations
	for _, candidate := range []string{"/opt/homebrew/bin/podman", "/usr/local/bin/podman"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "podman"
}

// containerToPodmanArgs builds a podman run argument list from a parsed [Container] section.
func containerToPodmanArgs(c map[string][]string) ([]string, error) {
	args := []string{podmanBinPath(), "run", "--rm"}

	if names := c["ContainerName"]; len(names) > 0 {
		// --replace removes any stale container with this name before starting
		args = append(args, "--name", names[0], "--replace")
	}
	for _, net := range c["Network"] {
		args = append(args, "--network", net)
	}
	for _, port := range c["PublishPort"] {
		args = append(args, "-p", port)
	}
	for _, vol := range c["Volume"] {
		args = append(args, "-v", expandSpecifiers(vol))
	}
	for _, env := range c["Environment"] {
		args = append(args, "-e", env)
	}
	for _, extra := range c["PodmanArgs"] {
		args = append(args, strings.Fields(expandSpecifiers(extra))...)
	}

	images := c["Image"]
	if len(images) == 0 {
		return nil, fmt.Errorf("no Image= found in [Container] section")
	}
	args = append(args, images[0])

	for _, cmd := range c["Exec"] {
		args = append(args, strings.Fields(cmd)...)
	}

	return args, nil
}

// --- Service unit files ---

func (m *darwinServiceManager) WriteServiceUnit(name, content string) error {
	svc := parseSection(content, "Service")
	execStarts := svc["ExecStart"]
	if len(execStarts) == 0 {
		return fmt.Errorf("no ExecStart= found in service unit %s", name)
	}
	// Expand %h and split into argv
	args := strings.Fields(expandSpecifiers(execStarts[0]))
	if len(args) == 0 {
		return fmt.Errorf("empty ExecStart in service unit %s", name)
	}

	if err := ensurePlistDirs(name); err != nil {
		return err
	}
	logPath := filepath.Join(lerdLogsDir(), name+".log")
	plist := buildPlist(plistLabel(name), args, false, false, logPath)
	return os.WriteFile(plistPath(name), []byte(plist), 0644)
}

func (m *darwinServiceManager) WriteServiceUnitIfChanged(name, content string) (bool, error) {
	// Derive what the plist content would be by writing to a temp path first.
	// Simpler: just check if the source content would produce a different plist.
	// We regenerate and compare against disk.
	svc := parseSection(content, "Service")
	execStarts := svc["ExecStart"]
	if len(execStarts) == 0 {
		return false, fmt.Errorf("no ExecStart= found in service unit %s", name)
	}
	args := strings.Fields(expandSpecifiers(execStarts[0]))
	if len(args) == 0 {
		return false, fmt.Errorf("empty ExecStart in service unit %s", name)
	}

	logPath := filepath.Join(lerdLogsDir(), name+".log")
	newPlist := buildPlist(plistLabel(name), args, false, false, logPath)

	if existing, err := os.ReadFile(plistPath(name)); err == nil && string(existing) == newPlist {
		return false, nil
	}
	if err := ensurePlistDirs(name); err != nil {
		return false, err
	}
	return true, os.WriteFile(plistPath(name), []byte(newPlist), 0644)
}

func (m *darwinServiceManager) RemoveServiceUnit(name string) error {
	if err := os.Remove(plistPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *darwinServiceManager) ListServiceUnits(nameGlob string) []string {
	pattern := filepath.Join(launchAgentsDir(), nameGlob+".plist")
	entries, _ := filepath.Glob(pattern)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(filepath.Base(e), ".plist"))
	}
	return names
}

// --- Container unit files ---

func (m *darwinServiceManager) WriteContainerUnit(name, content string) error {
	c := parseSection(content, "Container")
	args, err := containerToPodmanArgs(c)
	if err != nil {
		return fmt.Errorf("container unit %s: %w", name, err)
	}

	if err := ensurePlistDirs(name); err != nil {
		return err
	}
	logPath := filepath.Join(lerdLogsDir(), name+".log")
	plist := buildPlist(plistLabel(name), args, true, true, logPath)
	return os.WriteFile(plistPath(name), []byte(plist), 0644)
}

func (m *darwinServiceManager) ContainerUnitInstalled(name string) bool {
	_, err := os.Stat(plistPath(name))
	return err == nil
}

func (m *darwinServiceManager) RemoveContainerUnit(name string) error {
	if err := os.Remove(plistPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *darwinServiceManager) ListContainerUnits(nameGlob string) []string {
	// Container units share the same plist directory; no separate extension.
	// We use the same glob pattern as service units — callers are expected to
	// pass a glob that uniquely identifies containers (e.g. "lerd-*").
	return m.ListServiceUnits(nameGlob)
}

// --- Service lifecycle ---

// DaemonReload is a no-op on macOS; launchd picks up plist changes on load.
func (m *darwinServiceManager) DaemonReload() error { return nil }

func (m *darwinServiceManager) Start(name string) error {
	p := plistPath(name)
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("plist not found for %s", name)
	}
	cmd := exec.Command("launchctl", "load", p)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load %s: %w\n%s", name, err, out)
	}
	return nil
}

func (m *darwinServiceManager) Stop(name string) error {
	p := plistPath(name)
	cmd := exec.Command("launchctl", "unload", p)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// unload returns an error if the service isn't loaded — treat as success
		if strings.Contains(string(out), "Could not find specified") ||
			strings.Contains(string(out), "No such process") {
			return nil
		}
		return fmt.Errorf("launchctl unload %s: %w\n%s", name, err, out)
	}
	return nil
}

func (m *darwinServiceManager) Restart(name string) error {
	_ = m.Stop(name)
	return m.Start(name)
}

// Enable loads the plist with -w so it persists across reboots.
func (m *darwinServiceManager) Enable(name string) error {
	p := plistPath(name)
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("plist not found for %s", name)
	}
	cmd := exec.Command("launchctl", "load", "-w", p)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load -w %s: %w\n%s", name, err, out)
	}
	return nil
}

// Disable unloads the plist with -w so it won't restart at login.
func (m *darwinServiceManager) Disable(name string) error {
	p := plistPath(name)
	cmd := exec.Command("launchctl", "unload", "-w", p)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "Could not find specified") ||
			strings.Contains(string(out), "No such process") {
			return nil
		}
		return fmt.Errorf("launchctl unload -w %s: %w\n%s", name, err, out)
	}
	return nil
}

// IsActive returns true if the service is currently running (has a PID).
func (m *darwinServiceManager) IsActive(name string) bool {
	out, err := exec.Command("launchctl", "list", plistLabel(name)).Output()
	if err != nil {
		return false
	}
	// Output contains `"PID" = <number>;` when running
	return strings.Contains(string(out), `"PID"`)
}

// IsEnabled returns true if the plist exists in LaunchAgents.
// On macOS, placing a plist in ~/Library/LaunchAgents is the equivalent of "enabled".
func (m *darwinServiceManager) IsEnabled(name string) bool {
	_, err := os.Stat(plistPath(name))
	return err == nil
}

// UnitStatus returns a status string similar to systemd's active state.
func (m *darwinServiceManager) UnitStatus(name string) (string, error) {
	out, err := exec.Command("launchctl", "list", plistLabel(name)).Output()
	if err != nil {
		// Not loaded at all
		if _, statErr := os.Stat(plistPath(name)); statErr == nil {
			return "inactive", nil
		}
		return "unknown", nil
	}
	s := string(out)
	if strings.Contains(s, `"PID"`) {
		return "active", nil
	}
	// Loaded but not running — check last exit status
	if strings.Contains(s, `"LastExitStatus" = 0`) {
		return "inactive", nil
	}
	return "failed", nil
}
