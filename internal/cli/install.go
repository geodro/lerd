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
	phpDet "github.com/geodro/lerd/internal/php"
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

	// Ask before RunParallel steals stdin. Only offer the Laravel installer
	// when at least one PHP version is already installed — composer needs a
	// PHP runtime, and asking the question on a fresh install (where no
	// lerd-php*-fpm container exists) would just lead to a confusing failure.
	var wantLaravelInstaller bool
	if installedPHP, _ := phpDet.ListInstalled(); len(installedPHP) > 0 {
		wantLaravelInstaller = confirmInstallPrompt("Install Laravel installer (laravel new)?")
	}

	// 4. mkcert CA — interactive (may prompt for sudo)
	fmt.Println("  --> Installing mkcert CA")
	cmd := exec.Command(certs.MkcertPath(), "-install")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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
			// Skip paused and ignored sites — they have their own vhosts
			// (landing page or none) that should not be overwritten.
			if site.Paused || site.Ignored {
				continue
			}
			phpVer := site.PHPVersion
			if phpVer == "" && cfg != nil {
				phpVer = cfg.PHP.DefaultVersion
			}
			if site.Secured {
				if err := nginx.GenerateSSLVhost(site, phpVer); err != nil {
					fmt.Printf("\n    WARN %s: %v", site.PrimaryDomain(), err)
					continue
				}
				sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
				mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
				os.Remove(mainConf)          //nolint:errcheck
				os.Rename(sslConf, mainConf) //nolint:errcheck
			} else {
				if err := nginx.GenerateVhost(site, phpVer); err != nil {
					fmt.Printf("\n    WARN %s: %v", site.PrimaryDomain(), err)
				}
			}
		}
	}
	ok()

	// Note: WriteQuadlet centrally applies podman.BindForLAN based on
	// cfg.LAN.Exposed, so containers default to binding 127.0.0.1 unless
	// the user has run `lerd lan:expose on`. We use WriteQuadletDiff
	// (which reports whether the on-disk file actually changed) so we
	// can restart only the units whose binds shifted — important during
	// the upgrade from a pre-LAN-toggle release where nginx was bound to
	// 0.0.0.0 by default. Without the restart the running container
	// would silently keep its old LAN-exposed bind even though the
	// quadlet on disk now says 127.0.0.1.
	changedQuadlets := []string{}
	rewriteQuadlet := func(name string) error {
		content, err := podman.GetQuadletTemplate(name + ".container")
		if err != nil {
			return nil //nolint:nilerr // missing template = nothing to write
		}
		changed, err := podman.WriteQuadletDiff(name, content)
		if err != nil {
			return err
		}
		if changed {
			changedQuadlets = append(changedQuadlets, name)
		}
		return nil
	}

	step("Writing nginx quadlet")
	if err := rewriteQuadlet("lerd-nginx"); err != nil {
		return err
	}
	ok()

	step("Writing DNS quadlet")
	if err := rewriteQuadlet("lerd-dns"); err != nil {
		return err
	}
	ok()

	step("Refreshing service quadlets")
	for _, svc := range []string{"mysql", "redis", "postgres", "meilisearch", "rustfs", "mailpit"} {
		if !podman.QuadletInstalled("lerd-" + svc) {
			continue
		}
		_ = rewriteQuadlet("lerd-" + svc)
	}
	ok()

	// Always ensure the default PHP-FPM is available (needed for lerd new on fresh installs).
	// Then restore quadlets for any additional PHP versions and services from registered sites.
	{
		cfg, _ := config.LoadGlobal()
		seenPHP := map[string]bool{}
		seenSvc := map[string]bool{}

		if cfg != nil && cfg.PHP.DefaultVersion != "" {
			seenPHP[cfg.PHP.DefaultVersion] = true
			if err := ensureFPMQuadlet(cfg.PHP.DefaultVersion); err != nil {
				fmt.Printf("  WARN: default PHP %s FPM quadlet: %v\n", cfg.PHP.DefaultVersion, err)
			}
		}

		reg, regErr := config.LoadSites()
		if regErr == nil {

			for _, s := range reg.Sites {
				if s.Paused || s.Ignored {
					continue
				}

				// Restore FPM quadlet.
				v := s.PHPVersion
				if v == "" && cfg != nil {
					v = cfg.PHP.DefaultVersion
				}
				if v != "" && !seenPHP[v] {
					seenPHP[v] = true
					if err := ensureFPMQuadlet(v); err != nil {
						fmt.Printf("  WARN: PHP %s FPM quadlet: %v\n", v, err)
					}
				}

				// Restore service quadlets from .lerd.yaml.
				proj, _ := config.LoadProjectConfig(s.Path)
				if proj == nil {
					continue
				}
				for _, svc := range proj.Services {
					if seenSvc[svc.Name] {
						continue
					}
					seenSvc[svc.Name] = true
					if svc.Custom != nil {
						ensureCustomServiceQuadlet(svc.Custom) //nolint:errcheck
					} else {
						ensureServiceQuadlet(svc.Name) //nolint:errcheck
					}
				}
			}
		}
	}

	// 7. Pull images in parallel, then build dnsmasq.
	RunParallel([]BuildJob{ //nolint:errcheck
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
		{
			Label: "Building dnsmasq",
			Run: func(w io.Writer) error {
				containerfile := "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache dnsmasq\n"
				cmd := exec.Command("podman", "build", "-t", "lerd-dnsmasq:local", "-")
				cmd.Stdin = strings.NewReader(containerfile)
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		},
	})

	// 8. Systemd / services
	step("Reloading systemd daemon")
	if err := podman.DaemonReload(); err != nil {
		return err
	}
	ok()

	// Migration safety net: restart any container whose quadlet content
	// actually changed during this install run, EXCEPT lerd-nginx and
	// lerd-dns which are already restarted unconditionally below. The
	// scenario this catches: a user updating from a release where every
	// container was bound to 0.0.0.0 by default. Without this restart
	// the running services would silently keep their old LAN-exposed
	// bind even though the new quadlet on disk says 127.0.0.1.
	for _, name := range changedQuadlets {
		if name == "lerd-nginx" || name == "lerd-dns" {
			continue
		}
		if running, _ := podman.ContainerRunning(name); !running {
			continue
		}
		fmt.Printf("  --> Restarting %s (PublishPort changed) ", name)
		if err := podman.RestartUnit(name); err != nil {
			fmt.Printf("WARN: %v\n", err)
		} else {
			ok()
		}
	}

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

	fmt.Println("  --> Configuring DNS resolver")
	if err := dns.ConfigureResolver(); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}

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

	// Start restored services (mysql, redis, etc.) and workers.
	// Service quadlets were written earlier; now pull images and start them.
	// Then restore worker units from .lerd.yaml so everything comes back up.
	startRestoredServices()
	restoreSiteInfrastructure()

	if wantLaravelInstaller {
		fmt.Println("  --> Installing Laravel installer")
		if err := installLaravelInstaller(); err != nil {
			fmt.Printf("    WARN: %v\n", err)
		} else {
			fmt.Println("    OK")
		}
	}

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

	fmt.Println("\nLerd installation complete!")
	fmt.Println("\n  Dashboard: \033[96mhttp://lerd.localhost\033[0m")
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

// installLaravelInstaller runs composer global require laravel/installer
// directly inside an installed PHP-FPM container so the `laravel` CLI is
// available for scaffolding new apps. It bypasses the composer shim because
// the shim relies on cwd-based PHP detection, which does not work when
// install is invoked from a directory with no project metadata.
func installLaravelInstaller() error {
	installed, err := phpDet.ListInstalled()
	if err != nil || len(installed) == 0 {
		return fmt.Errorf("no PHP version installed — install one with `lerd php:install <version>` first")
	}

	// Prefer the configured default PHP, otherwise use the highest installed.
	version := installed[len(installed)-1]
	if cfg, _ := config.LoadGlobal(); cfg != nil && cfg.PHP.DefaultVersion != "" {
		for _, v := range installed {
			if v == cfg.PHP.DefaultVersion {
				version = v
				break
			}
		}
	}

	short := strings.ReplaceAll(version, ".", "")
	container := "lerd-php" + short + "-fpm"

	if running, _ := podman.ContainerRunning(container); !running {
		if err := podman.StartUnit(container); err != nil {
			return fmt.Errorf("starting %s: %w", container, err)
		}
	}

	home := os.Getenv("HOME")
	composerHome := os.Getenv("COMPOSER_HOME")
	if composerHome == "" {
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(home, ".config")
		}
		composerHome = filepath.Join(xdgConfig, "composer")
	}

	composerPhar := filepath.Join(config.BinDir(), "composer.phar")
	// --no-interaction prevents composer from blocking on plugin trust prompts
	// (e.g. "Do you trust 'symfony/flex' to execute code?") which would hang
	// the installer with no visible output.
	cmd := exec.Command("podman", "exec", "-i",
		"--env", "HOME="+home,
		"--env", "COMPOSER_HOME="+composerHome,
		container, "php", composerPhar, "global", "require", "--no-interaction", "laravel/installer",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// confirmInstallPrompt asks a [Y/n] question. Must be called before any
// RunParallel invocation, which leaves a goroutine reading from os.Stdin.
func confirmInstallPrompt(question string) bool {
	fmt.Printf("  --> %s [Y/n] ", question)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer != "n" && answer != "no"
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

	// Write laravel shim (laravel/installer global package)
	composerHome := os.Getenv("COMPOSER_HOME")
	if composerHome == "" {
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(home, ".config")
		}
		composerHome = filepath.Join(xdgConfig, "composer")
	}
	laravelShim := fmt.Sprintf("#!/bin/sh\nexec %s php %s/vendor/bin/laravel \"$@\"\n", lerdBin, composerHome)
	if err := os.WriteFile(filepath.Join(binDir, "laravel"), []byte(laravelShim), 0755); err != nil {
		return fmt.Errorf("writing laravel shim: %w", err)
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
	data, _ := os.ReadFile(rcFile)
	line := fmt.Sprintf("export PATH=\"%s:$PATH\"", binDir)
	if strings.Contains(string(data), line) {
		return nil
	}
	f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(fmt.Sprintf("\n# Lerd\n%s\n", line))
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
