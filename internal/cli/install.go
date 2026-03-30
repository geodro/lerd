package cli

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

func step(label string) { fmt.Printf("  --> %s ... ", label) }
func ok()               { fmt.Println("OK") }

func runInstall(_ *cobra.Command, _ []string) error {
	fmt.Println("==> Installing Lerd")

	if err := ensureUnprivilegedPorts(); err != nil {
		return err
	}

	// 1. Directories
	step("Creating directories")
	dirs := []string{
		config.ConfigDir(), config.DataDir(), config.BinDir(),
		config.NginxDir(), config.NginxConfD(), config.CertsDir(),
		filepath.Join(config.CertsDir(), "sites"),
		config.DnsmasqDir(), config.QuadletDir(), config.SystemdUserDir(),
		config.DataSubDir("mysql"), config.DataSubDir("redis"),
		config.DataSubDir("postgres"), config.DataSubDir("meilisearch"),
		config.DataSubDir("rustfs"), config.DataSubDir("mailpit"),
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
		return err
	}
	if err := podman.EnsureNetworkDNS("lerd", dns.ReadContainerDNS()); err != nil {
		return err
	}
	ok()

	// 3. Binaries (composer, fnm, mkcert)
	step("Downloading binaries")
	if err := downloadBinaries(os.Stdout); err != nil {
		return err
	}
	ok()

	// 4. mkcert CA — interactive (may prompt for sudo)
	fmt.Println("  --> Installing mkcert CA")
	cmd := exec.Command(certs.MkcertPath(), "-install")
	iw := &indentWriter{w: os.Stdout, prefix: "      ", bol: true}
	cmd.Stdin = os.Stdin
	cmd.Stdout = iw
	cmd.Stderr = iw
	cmd.Run() //nolint:errcheck

	// 5. DNS config + sudoers
	step("Writing DNS configuration")
	if err := dns.WriteDnsmasqConfig(config.DnsmasqDir()); err != nil {
		return err
	}
	ok()

	fmt.Println("  --> Installing DNS sudoers rule")
	dns.InstallSudoers() //nolint:errcheck

	// 6. Nginx
	step("Writing nginx configuration")
	if err := nginx.EnsureNginxConfig(); err != nil {
		return err
	}
	if err := nginx.EnsureDefaultVhost(); err != nil {
		return err
	}
	if err := nginx.EnsureLerdVhost(); err != nil {
		return err
	}
	ok()

	step("Regenerating vhosts")
	reg, err := config.LoadSites()
	if err == nil {
		cfg, _ := config.LoadGlobal()
		for _, site := range reg.Sites {
			phpVer := site.PHPVersion
			if phpVer == "" && cfg != nil {
				phpVer = cfg.PHP.DefaultVersion
			}
			if site.Secured {
				if err := nginx.GenerateSSLVhost(site, phpVer); err != nil {
					fmt.Printf("\n    WARN %s: %v", site.Domain, err)
					continue
				}
				sslConf := filepath.Join(config.NginxConfD(), site.Domain+"-ssl.conf")
				mainConf := filepath.Join(config.NginxConfD(), site.Domain+".conf")
				os.Remove(mainConf)          //nolint:errcheck
				os.Rename(sslConf, mainConf) //nolint:errcheck
			} else {
				if err := nginx.GenerateVhost(site, phpVer); err != nil {
					fmt.Printf("\n    WARN %s: %v", site.Domain, err)
				}
			}
		}
	}
	ok()

	step("Writing nginx quadlet")
	if content, err := podman.GetQuadletTemplate("lerd-nginx.container"); err == nil {
		if err := podman.WriteQuadlet("lerd-nginx", content); err != nil {
			return err
		}
	}
	ok()

	step("Writing DNS quadlet")
	if content, err := podman.GetQuadletTemplate("lerd-dns.container"); err == nil {
		if err := podman.WriteQuadlet("lerd-dns", content); err != nil {
			return err
		}
	}
	ok()

	step("Refreshing service quadlets")
	for _, svc := range []string{"mysql", "redis", "postgres", "meilisearch", "rustfs", "mailpit"} {
		if !podman.QuadletInstalled("lerd-" + svc) {
			continue
		}
		if content, err := podman.GetQuadletTemplate("lerd-" + svc + ".container"); err == nil {
			podman.WriteQuadlet("lerd-"+svc, content) //nolint:errcheck
		}
	}
	ok()

	// 7. Pull images in parallel, then build dnsmasq.
	pullJobs := []BuildJob{
		{
			Label: "Pulling nginx:alpine",
			Run: func(w io.Writer) error {
				cmd := exec.Command("podman", "pull", "docker.io/library/nginx:alpine")
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		},
		{
			Label: "Pulling alpine:latest",
			Run: func(w io.Writer) error {
				cmd := exec.Command("podman", "pull", "docker.io/library/alpine:latest")
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		},
	}
	if err := RunParallel(pullJobs); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}

	if err := RunParallel([]BuildJob{{
		Label: "Building dnsmasq image",
		Run: func(w io.Writer) error {
			containerfile := "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache dnsmasq\n"
			cmd := exec.Command("podman", "build", "-t", "lerd-dnsmasq:local", "-")
			cmd.Stdin = strings.NewReader(containerfile)
			cmd.Stdout = w
			cmd.Stderr = w
			return cmd.Run()
		},
	}}); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}

	// 8. Systemd / services
	step("Reloading systemd daemon")
	if err := podman.DaemonReload(); err != nil {
		return err
	}
	ok()

	step("Starting lerd-dns")
	if err := podman.RestartUnit("lerd-dns"); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}
	ok()

	step("Waiting for lerd-dns to be ready")
	if err := dns.WaitReady(15 * time.Second); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}
	ok()

	step("Configuring DNS resolver")
	if err := dns.ConfigureResolver(); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}
	ok()

	step("Starting lerd-nginx")
	if err := podman.RestartUnit("lerd-nginx"); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}
	ok()

	step("Writing watcher service")
	if content, err := lerdSystemd.GetUnit("lerd-watcher"); err == nil {
		if err := lerdSystemd.WriteService("lerd-watcher", content); err != nil {
			return err
		}
		if err := lerdSystemd.EnableService("lerd-watcher"); err != nil {
			fmt.Printf("    WARN: %v\n", err)
		}
	}
	ok()

	step("Restarting watcher service")
	if err := podman.RestartUnit("lerd-watcher"); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}
	ok()

	step("Writing UI service")
	if content, err := lerdSystemd.GetUnit("lerd-ui"); err == nil {
		if err := lerdSystemd.WriteService("lerd-ui", content); err != nil {
			return err
		}
		if err := lerdSystemd.EnableService("lerd-ui"); err != nil {
			fmt.Printf("    WARN: %v\n", err)
		}
	}
	ok()

	step("Starting lerd-ui")
	if err := podman.RestartUnit("lerd-ui"); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}
	ok()

	// Restart tray if running.
	if lerdSystemd.IsServiceEnabled("lerd-tray") {
		_ = lerdSystemd.RestartService("lerd-tray")
	} else {
		killTray()
		if exe, err := os.Executable(); err == nil {
			_ = exec.Command(exe, "tray").Start()
		}
	}

	step("Adding shell PATH configuration")
	if err := addShellShims(); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}
	ok()

	// Optional: register lerd MCP globally if Claude Code is installed.
	if _, err := exec.LookPath("claude"); err == nil {
		if !IsMCPGloballyRegistered() {
			fmt.Print("\n  --> Claude Code detected. Register lerd MCP globally? [Y/n] ")
			answer := ""
			if tty, err := os.Open("/dev/tty"); err == nil {
				reader := bufio.NewReader(tty)
				line, _ := reader.ReadString('\n')
				tty.Close()
				answer = strings.TrimSpace(line)
			}
			if answer == "" || strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") {
				if err := RunMCPEnableGlobal(); err != nil {
					fmt.Printf("    WARN: MCP registration failed: %v\n", err)
				}
			}
		}
	}

	fmt.Println("\nLerd installation complete!")
	fmt.Println("\n  Dashboard: \033[96mhttp://127.0.0.1:7073\033[0m")
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

	fmt.Print("  --> Setting net.ipv4.ip_unprivileged_port_start=80 ... ")
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
	fmt.Println("OK")
	return nil
}

func downloadBinaries(w io.Writer) error {
	arch := runtime.GOARCH
	binDir := config.BinDir()

	// composer
	composerPharPath := filepath.Join(binDir, "composer.phar")
	if _, err := os.Stat(composerPharPath); os.IsNotExist(err) {
		if err := downloadFile("https://getcomposer.org/composer-stable.phar", composerPharPath, 0755, w); err != nil {
			return fmt.Errorf("composer download: %w", err)
		}
	}

	// fnm
	fnmPath := filepath.Join(binDir, "fnm")
	if _, err := os.Stat(fnmPath); os.IsNotExist(err) {
		fnmZip := filepath.Join(binDir, "fnm-linux.zip")
		if err := downloadFile(
			"https://github.com/Schniz/fnm/releases/latest/download/fnm-linux.zip",
			fnmZip, 0644, w,
		); err != nil {
			return fmt.Errorf("fnm download: %w", err)
		}
		extractCmd := exec.Command("unzip", "-o", fnmZip, "fnm", "-d", binDir)
		extractCmd.Stdout = w
		extractCmd.Stderr = w
		if err := extractCmd.Run(); err != nil {
			return fmt.Errorf("fnm extract: %w", err)
		}
		os.Remove(fnmZip)
		os.Chmod(fnmPath, 0755) //nolint:errcheck
	}

	// mkcert
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
		if err := downloadFile(mkcertURL, mkcertPath, 0755, w); err != nil {
			return fmt.Errorf("mkcert download: %w", err)
		}
	}

	return nil
}

// downloadFile downloads a URL to a local file, printing a progress bar to w.
func downloadFile(url, dest string, mode os.FileMode, w io.Writer) error {
	fmt.Fprintf(w, "\n      Downloading %s\n      ", url)

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

	written, err := io.Copy(f, &progressReader{r: resp.Body, total: resp.ContentLength, w: w})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, " (%d bytes)\n", written)

	return os.Chmod(dest, mode)
}

// indentWriter prefixes each line of output with a fixed string.
type indentWriter struct {
	w      io.Writer
	prefix string
	bol    bool // beginning of line
}

func (iw *indentWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if iw.bol {
			fmt.Fprint(iw.w, iw.prefix)
			iw.bol = false
		}
		iw.w.Write([]byte{b}) //nolint:errcheck
		if b == '\n' {
			iw.bol = true
		}
	}
	return len(p), nil
}

type progressReader struct {
	r       io.Reader
	total   int64
	written int64
	w       io.Writer
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
		fmt.Fprintf(p.w, "\r      [%s] %d%%", bar, pct*2)
	}
	return n, err
}

func addShellShims() error {
	home, _ := os.UserHomeDir()
	binDir := config.BinDir()
	lerdBin := filepath.Join(home, ".local", "bin", "lerd")
	fnmBin := filepath.Join(binDir, "fnm")

	// Write php shim
	phpShim := fmt.Sprintf("#!/bin/sh\nexec %s php \"$@\"\n", lerdBin)
	if err := os.WriteFile(filepath.Join(binDir, "php"), []byte(phpShim), 0755); err != nil {
		return fmt.Errorf("writing php shim: %w", err)
	}

	// Write composer shim
	composerShim := fmt.Sprintf("#!/bin/sh\nexec %s php %s/.local/share/lerd/bin/composer.phar \"$@\"\n", lerdBin, home)
	if err := os.WriteFile(filepath.Join(binDir, "composer"), []byte(composerShim), 0755); err != nil {
		return fmt.Errorf("writing composer shim: %w", err)
	}

	// Write node/npm/npx shims — use fnm directly so they work inside containers
	// (lerd is glibc-linked and cannot run inside Alpine-based PHP containers).
	nodeShimTmpl := `#!/bin/sh
FNM="%s"
VERSION=""
for f in .node-version .nvmrc; do
  [ -f "$f" ] && VERSION=$(tr -d '[:space:]' < "$f") && break
done
if [ -n "$VERSION" ]; then
  "$FNM" install "$VERSION" >/dev/null 2>&1 || true
  exec "$FNM" exec --using="$VERSION" -- %s "$@"
else
  exec "$FNM" exec --using=default -- %s "$@"
fi
`
	for _, bin := range []string{"node", "npm", "npx"} {
		shim := fmt.Sprintf(nodeShimTmpl, fnmBin, bin, bin)
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
		if err := os.WriteFile(fishConf, []byte(content), 0644); err != nil {
			return err
		}
		installCompletion(lerdBin, "fish", filepath.Join(home, ".config", "fish", "completions"), "lerd.fish")
		return nil
	case isShell(shell, "zsh"):
		if err := appendShellRC(filepath.Join(home, ".zshrc"), binDir); err != nil {
			return err
		}
		zshFunctionsDir := filepath.Join(home, ".local", "share", "zsh", "site-functions")
		if err := os.MkdirAll(zshFunctionsDir, 0755); err == nil {
			installCompletion(lerdBin, "zsh", zshFunctionsDir, "_lerd")
			appendShellRC(filepath.Join(home, ".zshrc"), "") // ensure fpath line exists
			ensureZshFpath(filepath.Join(home, ".zshrc"), zshFunctionsDir)
		}
		return nil
	default:
		if err := appendShellRC(filepath.Join(home, ".bashrc"), binDir); err != nil {
			return err
		}
		bashCompDir := filepath.Join(home, ".local", "share", "bash-completion", "completions")
		if err := os.MkdirAll(bashCompDir, 0755); err == nil {
			installCompletion(lerdBin, "bash", bashCompDir, "lerd")
		}
		return nil
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

// installCompletion generates and writes a shell completion script for lerd.
func installCompletion(lerdBin, shell, dir, filename string) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	out, err := exec.Command(lerdBin, "completion", shell).Output()
	if err != nil {
		return
	}
	os.WriteFile(filepath.Join(dir, filename), out, 0644) //nolint:errcheck
}

// ensureZshFpath appends a fpath line for dir to the zshrc if not already present.
func ensureZshFpath(zshrc, dir string) {
	data, _ := os.ReadFile(zshrc)
	line := fmt.Sprintf("fpath=(%s $fpath)", dir)
	if strings.Contains(string(data), line) {
		return
	}
	f, err := os.OpenFile(zshrc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n# Lerd completions\n%s\nautoload -Uz compinit && compinit\n", line)
}
