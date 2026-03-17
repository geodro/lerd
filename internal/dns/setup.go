package dns

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
)

const dnsmasqConf = `# Lerd DNS configuration
address=/.test/127.0.0.1
port=53
`

const nmDnsConf = `[main]
dns=dnsmasq
`

const nmDnsmasqConf = `server=/test/127.0.0.1#5300
`

// isFileContent returns true if the file at path already contains exactly content.
func isFileContent(path string, content []byte) bool {
	existing, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return string(existing) == string(content)
}

// Setup writes NetworkManager dnsmasq configuration and restarts NetworkManager.
// Skips sudo steps if the files are already correctly configured.
func Setup() error {
	if err := WriteDnsmasqConfig(config.DnsmasqDir()); err != nil {
		return fmt.Errorf("writing lerd dnsmasq config: %w", err)
	}

	nmConfFile := "/etc/NetworkManager/conf.d/lerd.conf"
	nmDnsmasqFile := "/etc/NetworkManager/dnsmasq.d/lerd.conf"

	// If both files are already correct, skip sudo entirely.
	if isFileContent(nmConfFile, []byte(nmDnsConf)) && isFileContent(nmDnsmasqFile, []byte(nmDnsmasqConf)) {
		return nil
	}

	fmt.Println("  [sudo required] Configuring NetworkManager for .test DNS resolution")

	if err := sudoWriteFile(nmConfFile, []byte(nmDnsConf)); err != nil {
		return fmt.Errorf("writing NetworkManager conf: %w", err)
	}

	if err := sudoWriteFile(nmDnsmasqFile, []byte(nmDnsmasqConf)); err != nil {
		return fmt.Errorf("writing NetworkManager dnsmasq conf: %w", err)
	}

	// Restart NetworkManager
	cmd := exec.Command("sudo", "systemctl", "restart", "NetworkManager")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("restarting NetworkManager: %w", err)
	}
	return nil
}

// WriteDnsmasqConfig writes the lerd dnsmasq config to the given directory.
func WriteDnsmasqConfig(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "lerd.conf"), []byte(dnsmasqConf), 0644)
}

// sudoWriteFile writes content to a system path by writing to a temp file
// then using sudo cp, so sudo can prompt for a password on the terminal.
func sudoWriteFile(path string, content []byte) error {
	tmp, err := os.CreateTemp("", "lerd-sudo-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	dir := filepath.Dir(path)
	mkdirCmd := exec.Command("sudo", "mkdir", "-p", dir)
	mkdirCmd.Stdin = os.Stdin
	mkdirCmd.Stdout = os.Stdout
	mkdirCmd.Stderr = os.Stderr
	if err := mkdirCmd.Run(); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	cpCmd := exec.Command("sudo", "cp", tmp.Name(), path)
	cpCmd.Stdin = os.Stdin
	cpCmd.Stdout = os.Stdout
	cpCmd.Stderr = os.Stderr
	if err := cpCmd.Run(); err != nil {
		return fmt.Errorf("cp to %s: %w", path, err)
	}
	return nil
}
