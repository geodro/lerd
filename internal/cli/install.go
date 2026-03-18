package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/spf13/cobra"
)

// NewInstallCmd returns the install command.
func NewInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Run one-time Lerd setup",
		RunE:  runInstall,
	}
}

func runInstall(_ *cobra.Command, _ []string) error {
	fmt.Println("==> Installing Lerd")

	// 0. Check unprivileged port binding (needed for nginx on 80/443)
	if err := ensureUnprivilegedPorts(); err != nil {
		return err
	}

	// 1. Create XDG directories
	step("Creating directories")
	dirs := []string{
		config.ConfigDir(),
		config.DataDir(),
		config.BinDir(),
		config.NginxDir(),
		config.NginxConfD(),
		config.CertsDir(),
		filepath.Join(config.CertsDir(), "sites"),
		config.DnsmasqDir(),
		config.QuadletDir(),
		config.SystemdUserDir(),
		config.DataSubDir("mysql"),
		config.DataSubDir("redis"),
		config.DataSubDir("postgres"),
		config.DataSubDir("meilisearch"),
		config.DataSubDir("minio"),
		config.DataSubDir("mailpit"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}
	ok()

	// 2. Podman network
	step("Creating lerd podman network")
	if err := podman.EnsureNetwork("lerd"); err != nil {
		return fmt.Errorf("podman network: %w", err)
	}
	ok()

	// 3. Download binaries
	if err := downloadBinaries(); err != nil {
		return err
	}

	// 4. mkcert -install
	step("Installing mkcert CA")
	if err := certs.InstallCA(); err != nil {
		return fmt.Errorf("mkcert CA: %w", err)
	}
	ok()

	// 5. DNS config file (written early so the container has it on first start)
	step("Writing DNS configuration")
	if err := dns.WriteDnsmasqConfig(config.DnsmasqDir()); err != nil {
		return fmt.Errorf("dns config: %w", err)
	}
	ok()

	step("Installing DNS sudoers rule")
	if err := dns.InstallSudoers(); err != nil {
		fmt.Printf(" [WARN: %v]\n", err)
	} else {
		ok()
	}

	// 6. Nginx config and quadlet
	step("Writing nginx configuration")
	if err := nginx.EnsureNginxConfig(); err != nil {
		return fmt.Errorf("nginx config: %w", err)
	}
	ok()

	step("Regenerating vhosts")
	if reg, err := config.LoadSites(); err == nil {
		cfg, _ := config.LoadGlobal()
		for _, site := range reg.Sites {
			phpVer := site.PHPVersion
			if phpVer == "" && cfg != nil {
				phpVer = cfg.PHP.DefaultVersion
			}
			if site.Secured {
				// Regenerate SSL vhost in-place: write -ssl.conf then rename to .conf
				if err := nginx.GenerateSSLVhost(site, phpVer); err != nil {
					fmt.Printf(" [WARN %s: %v]", site.Domain, err)
					continue
				}
				sslConf := filepath.Join(config.NginxConfD(), site.Domain+"-ssl.conf")
				mainConf := filepath.Join(config.NginxConfD(), site.Domain+".conf")
				os.Remove(mainConf)                     //nolint:errcheck
				if err := os.Rename(sslConf, mainConf); err != nil {
					fmt.Printf(" [WARN %s: %v]", site.Domain, err)
				}
			} else {
				if err := nginx.GenerateVhost(site, phpVer); err != nil {
					fmt.Printf(" [WARN %s: %v]", site.Domain, err)
				}
			}
		}
	}
	ok()

	step("Writing nginx quadlet")
	nginxQuadlet, err := podman.GetQuadletTemplate("lerd-nginx.container")
	if err != nil {
		return err
	}
	if err := podman.WriteQuadlet("lerd-nginx", nginxQuadlet); err != nil {
		return fmt.Errorf("nginx quadlet: %w", err)
	}
	ok()

	// Write DNS quadlet
	step("Writing DNS quadlet")
	dnsQuadlet, err := podman.GetQuadletTemplate("lerd-dns.container")
	if err != nil {
		return err
	}
	if err := podman.WriteQuadlet("lerd-dns", dnsQuadlet); err != nil {
		return fmt.Errorf("dns quadlet: %w", err)
	}
	ok()

	// Refresh any already-installed service quadlets so image names etc. stay current.
	step("Refreshing installed service quadlets")
	for _, svc := range []string{"mysql", "redis", "postgres", "meilisearch", "minio", "mailpit", "soketi"} {
		if !podman.QuadletInstalled("lerd-" + svc) {
			continue
		}
		content, err := podman.GetQuadletTemplate("lerd-" + svc + ".container")
		if err != nil {
			continue
		}
		podman.WriteQuadlet("lerd-"+svc, content) //nolint:errcheck
	}
	ok()

	// 7. Pre-pull container images and pre-build lerd-dnsmasq so services start instantly
	step("Pulling container images")
	for _, image := range []string{"docker.io/library/nginx:alpine", "docker.io/library/alpine:latest"} {
		cmd := exec.Command("podman", "pull", image)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf(" [WARN pulling %s: %v]\n", image, err)
		}
	}
	ok()

	// Pre-build dnsmasq image so lerd-dns starts instantly without downloading at runtime.
	step("Building dnsmasq image")
	containerfile := "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache dnsmasq\n"
	buildCmd := exec.Command("podman", "build", "-t", "lerd-dnsmasq:local", "-")
	buildCmd.Stdin = strings.NewReader(containerfile)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Printf(" [WARN building dnsmasq image: %v]\n", err)
	} else {
		ok()
	}

	// 8. daemon-reload and start services
	step("Reloading systemd daemon")
	if err := podman.DaemonReload(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	ok()

	step("Starting lerd-dns")
	if err := podman.RestartUnit("lerd-dns"); err != nil {
		fmt.Printf(" [WARN: %v]\n", err)
	} else {
		ok()
	}

	// Configure system resolver now that dnsmasq is running, so applying resolvectl
	// immediately doesn't break DNS before the container is up.
	step("Configuring DNS resolver")
	if err := dns.ConfigureResolver(); err != nil {
		fmt.Printf(" [WARN: %v]\n", err)
	} else {
		ok()
	}

	// Write UI vhost before starting nginx so it's available on first start.
	step("Writing UI vhost")
	// host.containers.internal is a Podman built-in hostname that always resolves
	// to the host from inside any container, bypassing firewall rules on the gateway IP.
	if err := nginx.GenerateProxyVhost("lerd.test", "host.containers.internal", 7073); err != nil {
		fmt.Printf(" [WARN: %v]\n", err)
	} else {
		ok()
	}

	step("Starting lerd-nginx")
	if err := podman.RestartUnit("lerd-nginx"); err != nil {
		fmt.Printf(" [WARN: %v]\n", err)
	} else {
		ok()
	}

	// 8. Watcher service
	step("Writing watcher service")
	watcherContent, err := lerdSystemd.GetUnit("lerd-watcher")
	if err != nil {
		return err
	}
	if err := lerdSystemd.WriteService("lerd-watcher", watcherContent); err != nil {
		return fmt.Errorf("watcher service: %w", err)
	}
	if err := lerdSystemd.EnableService("lerd-watcher"); err != nil {
		fmt.Printf(" [WARN: %v]\n", err)
	} else {
		ok()
	}

	// UI service
	step("Writing UI service")
	uiContent, err := lerdSystemd.GetUnit("lerd-ui")
	if err != nil {
		return err
	}
	if err := lerdSystemd.WriteService("lerd-ui", uiContent); err != nil {
		return fmt.Errorf("ui service: %w", err)
	}
	if err := lerdSystemd.EnableService("lerd-ui"); err != nil {
		fmt.Printf(" [WARN: %v]\n", err)
	} else {
		ok()
	}

	step("Starting lerd-ui")
	if err := podman.RestartUnit("lerd-ui"); err != nil {
		fmt.Printf(" [WARN: %v]\n", err)
	} else {
		ok()
	}

	// 9. Shell shims
	step("Adding shell PATH configuration")
	if err := addShellShims(); err != nil {
		fmt.Printf(" [WARN: %v]\n", err)
	} else {
		ok()
	}

	fmt.Println("\nLerd installation complete!")
	fmt.Println("\n  Dashboard: \033[96mhttp://lerd.test\033[0m  \033[2m(or \033[0m\033[96mhttp://127.0.0.1:7073\033[0m\033[2m)\033[0m")
	return nil
}

// ensureUnprivilegedPorts checks net.ipv4.ip_unprivileged_port_start and
// offers to set it to 80 so rootless Podman can bind to ports 80 and 443.
func ensureUnprivilegedPorts() error {
	const sysctlPath = "/proc/sys/net/ipv4/ip_unprivileged_port_start"
	data, err := os.ReadFile(sysctlPath)
	if err != nil {
		// Not available on this kernel — skip
		return nil
	}
	val := 1024
	fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &val)
	if val <= 80 {
		return nil // already fine
	}

	fmt.Printf("\n  ! Port 80/443 require net.ipv4.ip_unprivileged_port_start ≤ 80 (current: %d)\n", val)
	fmt.Println("    This is needed for rootless Podman to run Nginx on standard HTTP/HTTPS ports.")

	step("Setting net.ipv4.ip_unprivileged_port_start=80")
	cmds := [][]string{
		{"sudo", "sysctl", "-w", "net.ipv4.ip_unprivileged_port_start=80"},
		{"sudo", "sh", "-c", "echo 'net.ipv4.ip_unprivileged_port_start=80' > /etc/sysctl.d/99-lerd-ports.conf"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("setting unprivileged port start: %w", err)
		}
	}
	ok()
	return nil
}

func step(msg string) {
	fmt.Printf("  --> %s ... ", msg)
}

func ok() {
	fmt.Println("OK")
}

func downloadBinaries() error {
	arch := runtime.GOARCH
	binDir := config.BinDir()

	// composer
	step("Downloading composer")
	composerPharPath := filepath.Join(binDir, "composer.phar")
	if _, err := os.Stat(composerPharPath); os.IsNotExist(err) {
		if err := downloadFile("https://getcomposer.org/composer-stable.phar", composerPharPath, 0755); err != nil {
			return fmt.Errorf("composer download: %w", err)
		}
	}
	ok()

	// fnm
	step("Downloading fnm")
	fnmPath := filepath.Join(binDir, "fnm")
	if _, err := os.Stat(fnmPath); os.IsNotExist(err) {
		fnmZip := filepath.Join(binDir, "fnm-linux.zip")
		if err := downloadFile(
			"https://github.com/Schniz/fnm/releases/latest/download/fnm-linux.zip",
			fnmZip, 0644,
		); err != nil {
			return fmt.Errorf("fnm download: %w", err)
		}
		extractCmd := exec.Command("unzip", "-o", fnmZip, "fnm", "-d", binDir)
		if out, err := extractCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("fnm extract: %w\n%s", err, out)
		}
		os.Remove(fnmZip)
		os.Chmod(fnmPath, 0755) //nolint:errcheck
	}
	ok()

	// mkcert
	step("Downloading mkcert")
	mkcertPath := certs.MkcertPath()
	if _, err := os.Stat(mkcertPath); os.IsNotExist(err) {
		mkcertArch := "amd64"
		if arch == "arm64" {
			mkcertArch = "arm64"
		}
		mkcertURL := fmt.Sprintf(
			"https://github.com/FiloSottile/mkcert/releases/latest/download/mkcert-v1.4.4-linux-%s",
			mkcertArch,
		)
		if err := downloadFile(mkcertURL, mkcertPath, 0755); err != nil {
			return fmt.Errorf("mkcert download: %w", err)
		}
	}
	ok()

	return nil
}

// downloadFile downloads a URL to a local file with progress.
func downloadFile(url, dest string, mode os.FileMode) error {
	fmt.Printf("\n      Downloading %s\n      ", url)

	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()

	written, err := io.Copy(f, &progressReader{r: resp.Body, total: resp.ContentLength})
	if err != nil {
		return err
	}
	fmt.Printf(" (%d bytes)\n      ", written)

	return os.Chmod(dest, mode)
}

type progressReader struct {
	r       io.Reader
	total   int64
	written int64
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.written += int64(n)
	if p.total > 0 {
		pct := int(float64(p.written) / float64(p.total) * 50)
		bar := ""
		for i := 0; i < 50; i++ {
			if i < pct {
				bar += "="
			} else {
				bar += " "
			}
		}
		fmt.Printf("\r      [%s] %d%%", bar, pct*2)
	}
	return n, err
}

func addShellShims() error {
	home, _ := os.UserHomeDir()
	binDir := config.BinDir()

	// Write php shim
	phpShim := "#!/bin/sh\nexec lerd php \"$@\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "php"), []byte(phpShim), 0755); err != nil {
		return fmt.Errorf("writing php shim: %w", err)
	}

	// Write composer shim
	composerShim := fmt.Sprintf("#!/bin/sh\nexec lerd php %s/.local/share/lerd/bin/composer.phar \"$@\"\n", home)
	if err := os.WriteFile(filepath.Join(binDir, "composer"), []byte(composerShim), 0755); err != nil {
		return fmt.Errorf("writing composer shim: %w", err)
	}

	// Write node/npm/npx shims
	for _, bin := range []string{"node", "npm", "npx"} {
		shim := fmt.Sprintf("#!/bin/sh\nexec lerd %s \"$@\"\n", bin)
		if err := os.WriteFile(filepath.Join(binDir, bin), []byte(shim), 0755); err != nil {
			return fmt.Errorf("writing %s shim: %w", bin, err)
		}
	}

	shell := os.Getenv("SHELL")

	switch {
	case isShell(shell, "fish"):
		fishConfigDir := filepath.Join(home, ".config", "fish", "conf.d")
		if err := os.MkdirAll(fishConfigDir, 0755); err != nil {
			return err
		}
		fishConf := filepath.Join(fishConfigDir, "lerd.fish")
		content := fmt.Sprintf("set -gx PATH %s $PATH\n", binDir)
		return os.WriteFile(fishConf, []byte(content), 0644)
	case isShell(shell, "zsh"):
		return appendShellRC(filepath.Join(home, ".zshrc"), binDir)
	default:
		return appendShellRC(filepath.Join(home, ".bashrc"), binDir)
	}
}

func appendShellRC(rcFile, binDir string) error {
	line := fmt.Sprintf("\n# Lerd\nexport PATH=\"%s:$PATH\"\n", binDir)
	f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

func isShell(shell, name string) bool {
	return len(shell) > 0 && filepath.Base(shell) == name
}
