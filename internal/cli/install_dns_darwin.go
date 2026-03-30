//go:build darwin

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/services"
)

// installDNSService installs native dnsmasq on macOS via Homebrew.
// Running dnsmasq natively (not in a Podman container) avoids the gvproxy
// UDP forwarding limitation present in Podman Machine on macOS.
func installDNSService(w io.Writer) error {
	binary, err := findDnsmasq()
	if err != nil {
		// Not installed — try to install via Homebrew.
		if _, berr := exec.LookPath("brew"); berr != nil {
			return fmt.Errorf("dnsmasq not found and Homebrew not available — install dnsmasq manually")
		}
		fmt.Fprintln(w, "==> Installing dnsmasq via Homebrew")
		cmd := exec.Command("brew", "install", "dnsmasq")
		cmd.Stdout = w
		cmd.Stderr = w
		if cerr := cmd.Run(); cerr != nil {
			return fmt.Errorf("brew install dnsmasq: %w", cerr)
		}
		binary, err = findDnsmasq()
		if err != nil {
			return fmt.Errorf("dnsmasq still not found after brew install: %w", err)
		}
	}

	// Bootout any existing lerd-dns service so that the new native plist
	// is picked up by the next bootstrap. Without this, kickstart would
	// restart the already-loaded (container-based) definition.
	label := "com.lerd.lerd-dns"
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	exec.Command("launchctl", "bootout", domain+"/"+label).Run() //nolint:errcheck

	// Stop and remove any running lerd-dns container. Container units use
	// --restart=always which keeps the container alive independently of launchd;
	// it must be removed before native dnsmasq can bind to port 5300.
	exec.Command("podman", "stop", "lerd-dns").Run()  //nolint:errcheck
	exec.Command("podman", "rm", "-f", "lerd-dns").Run() //nolint:errcheck

	// Write the launchd plist for lerd-dns using the native dnsmasq binary.
	// KeepAlive=true keeps dnsmasq running; the service unit mechanism uses
	// KeepAlive=false because container services use --restart=always.
	dnsmasqDir := config.DnsmasqDir()
	logDir := filepath.Join(os.Getenv("HOME"), "Library", "Logs", "lerd")
	logPath := filepath.Join(logDir, "lerd-dns.log")
	plistFile := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "lerd-dns.plist")

	args := []string{binary, "--no-daemon", "--conf-dir=" + dnsmasqDir}
	plist := buildNativePlist(label, args, logPath)

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistFile), 0755); err != nil {
		return err
	}
	return os.WriteFile(plistFile, []byte(plist), 0644)
}

// needsDNSServiceInstall returns true if the DNS plist hasn't been written yet
// OR if it still references the old container-based approach (podman run).
func needsDNSServiceInstall() bool {
	plistFile := filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "lerd-dns.plist")
	data, err := os.ReadFile(plistFile)
	if err != nil {
		return true // not yet written
	}
	// If the plist still calls podman, we need to replace it with native dnsmasq.
	return strings.Contains(string(data), "podman")
}

// findDnsmasq returns the path to the dnsmasq binary on macOS.
func findDnsmasq() (string, error) {
	if p, err := exec.LookPath("dnsmasq"); err == nil {
		return p, nil
	}
	for _, candidate := range []string{
		"/opt/homebrew/sbin/dnsmasq", // Apple Silicon Homebrew
		"/usr/local/sbin/dnsmasq",    // Intel Homebrew
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("dnsmasq not found")
}

// buildNativePlist builds a launchd plist for a long-running daemon (KeepAlive=true).
func buildNativePlist(label string, args []string, logPath string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>`)
	sb.WriteString(label)
	sb.WriteString("</string>\n\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range args {
		sb.WriteString("\t\t<string>")
		sb.WriteString(a)
		sb.WriteString("</string>\n")
	}
	sb.WriteString("\t</array>\n")
	sb.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	sb.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	if logPath != "" {
		sb.WriteString("\t<key>StandardOutPath</key>\n\t<string>")
		sb.WriteString(logPath)
		sb.WriteString("</string>\n\t<key>StandardErrorPath</key>\n\t<string>")
		sb.WriteString(logPath)
		sb.WriteString("</string>\n")
	}
	sb.WriteString("</dict>\n</plist>\n")
	return sb.String()
}

// ensureDNSImage is a no-op on macOS — DNS runs natively, no image needed.
func ensureDNSImageForStart() {}

// pullDNSImages is a no-op on macOS — DNS runs natively.
func pullDNSImages() []BuildJob { return nil }

// isDNSContainerUnit returns false on macOS since DNS uses a native service.
func isDNSContainerUnit() bool { return false }

// getInstalledDNSManager returns the service manager for DNS on macOS,
// using the native dnsmasq rather than a container.
func writeDNSUnit(_ io.Writer) error {
	return installDNSService(io.Discard)
}

// ensureDNSServiceUpdated rewrites the DNS plist if it still uses the old
// container-based approach, migrating to native dnsmasq automatically.
func ensureDNSServiceUpdated(w io.Writer) error {
	if needsDNSServiceInstall() {
		fmt.Fprintln(w, "  --> Migrating DNS from container to native dnsmasq ...")
		return installDNSService(w)
	}
	return nil
}

// removeDNSContainerIfRunning stops and removes the legacy lerd-dns container
// if it's still running (migration from container-based to native DNS).
func removeDNSContainerIfRunning() {
	exec.Command("podman", "stop", "lerd-dns").Run()  //nolint:errcheck
	exec.Command("podman", "rm", "-f", "lerd-dns").Run() //nolint:errcheck
}

// nativeDNSRestart restarts the native dnsmasq launchd service.
func nativeDNSRestart() error {
	return services.Mgr.Restart("lerd-dns")
}
