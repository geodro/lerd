package cli

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/nginx"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
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

	// On macOS, Podman Machine must be running before any podman commands.
	ensurePodmanMachineRunning()

	if err := ensureUnprivilegedPorts(); err != nil {
		return err
	}
	if err := ensurePortForwarding(); err != nil {
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

	// 1b. Enable systemd linger so user services (lerd-dns, lerd-nginx, the
	// PHP-FPM containers) survive screen blank, lock, and logout. Without
	// linger, Ubuntu/GNOME tears down the rootless Podman containers when
	// the session goes inactive and lerd appears to "stop working" until
	// the next manual `lerd install`. This is the single biggest source of
	// "DNS just stopped" issues reported in the wild — see #153.
	if err := ensureSystemdLinger(); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}

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
	// Skip the prompt entirely when laravel/installer is already present in
	// the user's composer global vendor dir, since re-running install should
	// not pester the user about something that is already set up.
	var wantLaravelInstaller bool
	if installedPHP, _ := phpDet.ListInstalled(); len(installedPHP) > 0 && !laravelInstallerPresent() {
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
	extraVolumes := podman.ExtraVolumePaths()
	globalCfg, _ := config.LoadGlobal()
	rewriteQuadlet := func(name string) error {
		content, err := podman.GetQuadletTemplate(name + ".container")
		if err != nil {
			return nil //nolint:nilerr // missing template = nothing to write
		}
		content = podman.InjectExtraVolumes(content, extraVolumes)
		svcName := strings.TrimPrefix(name, "lerd-")
		// Match ensureServiceQuadlet so install and the runtime service
		// path produce byte-identical files. Without this, a user who has
		// pinned an image (e.g. `mysql:8.0` instead of the embed's
		// `docker.io/library/mysql:8.0`) sees a perpetual diff and the
		// "PublishPort changed" restart fires on every install.
		if globalCfg != nil {
			if svcCfg, ok := globalCfg.Services[svcName]; ok {
				content = podman.ApplyImage(content, svcCfg.Image)
				if len(svcCfg.ExtraPorts) > 0 {
					content = podman.ApplyExtraPorts(content, svcCfg.ExtraPorts)
				}
			}
		}
		// Platform override applied last so it wins over the global config image.
		if currentImage := podman.CurrentImage(content); currentImage != "" {
			if override := platformImageOverride(svcName, currentImage); override != "" {
				content = podman.ApplyImage(content, override)
			}
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

	step("Writing DNS service unit")
	if err := writeDNSUnit(os.Stdout); err != nil {
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

	// On macOS, DNS runs natively (no container image needed) and DaemonReload
	// is a no-op, so we can start lerd-dns and configure the resolver here —
	// before RunParallel — keeping all sudo prompts before the image-pull spinner.
	if !isDNSContainerUnit() {
		step("Starting lerd-dns")
		if err := services.Mgr.Restart("lerd-dns"); err != nil {
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
	}

	// 7. Pull images sequentially, then build dnsmasq. Sequential output
	// keeps any sudo password prompt from later steps visible instead of
	// being clobbered by a live-updating spinner.
	pullJobs := []BuildJob{
		{
			Label: "Pulling nginx:alpine",
			Run: func(w io.Writer) error {
				cmd := podman.Cmd("pull", "docker.io/library/nginx:alpine")
				cmd.Stdout = w
				cmd.Stderr = w
				return cmd.Run()
			},
		},
	}
	pullJobs = append(pullJobs, pullDNSImages()...)
	for _, job := range pullJobs {
		step(job.Label)
		if err := job.Run(io.Discard); err != nil {
			fmt.Printf("WARN: %v\n", err)
			continue
		}
		ok()
	}

	// 8. Systemd / services
	step("Reloading service manager")
	if err := services.Mgr.DaemonReload(); err != nil {
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
		if err := services.Mgr.Restart(name); err != nil {
			fmt.Printf("WARN: %v\n", err)
		} else {
			ok()
		}
	}

	// On Linux, DNS is a container — start it after images are pulled.
	// On macOS it was already started before RunParallel above.
	if isDNSContainerUnit() {
		step("Starting lerd-dns")
		if err := services.Mgr.Restart("lerd-dns"); err != nil {
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
	}

	// Read the autostart flag once. When disabled (set explicitly via
	// `lerd autostart disable`), install must not enable or start any
	// service that the user has chosen to keep off — otherwise running
	// `lerd update` would silently flip every disabled unit back on.
	// The zero value (Disabled=false) is the historical autostart-on
	// path, so existing users see no behaviour change.
	autostartOn := lerdSystemd.IsAutostartEnabled()

	if autostartOn {
		step("Starting lerd-nginx")
		if err := services.Mgr.Restart("lerd-nginx"); err != nil {
			fmt.Printf("    WARN: %v\n", err)
		}
		ok()
	}

	step("Writing watcher service")
	if content, err := lerdSystemd.GetUnit("lerd-watcher"); err == nil {
		if err := services.Mgr.WriteServiceUnit("lerd-watcher", content); err != nil {
			return err
		}
		if autostartOn {
			if err := services.Mgr.Enable("lerd-watcher"); err != nil {
				fmt.Printf("    WARN: %v\n", err)
			}
		}
	}
	ok()

	if autostartOn {
		step("Restarting watcher service")
		if err := services.Mgr.Restart("lerd-watcher"); err != nil {
			fmt.Printf("    WARN: %v\n", err)
		}
		ok()
	}

	step("Writing UI service")
	if content, err := lerdSystemd.GetUnit("lerd-ui"); err == nil {
		if err := services.Mgr.WriteServiceUnit("lerd-ui", content); err != nil {
			return err
		}
		if autostartOn {
			if err := services.Mgr.Enable("lerd-ui"); err != nil {
				fmt.Printf("    WARN: %v\n", err)
			}
		}
	}
	ok()

	if autostartOn {
		step("Starting lerd-ui")
		if err := services.Mgr.Restart("lerd-ui"); err != nil {
			fmt.Printf("    WARN: %v\n", err)
		}
		ok()
	}

	step("Writing tray service")
	if content, err := lerdSystemd.GetUnit("lerd-tray"); err == nil {
		if err := services.Mgr.WriteServiceUnit("lerd-tray", content); err != nil {
			return err
		}
		if autostartOn {
			if err := services.Mgr.Enable("lerd-tray"); err != nil {
				fmt.Printf("    WARN: %v\n", err)
			}
		}
	}
	ok()

	// Restore worker / queue / schedule unit FILES from .lerd.yaml so the
	// systemd state is repaired regardless of the autostart setting — the
	// files have to exist for the user to be able to flip autostart back
	// on later. restoreSiteInfrastructure only writes files for units
	// that don't already exist, so this is a no-op for ordinary updates.
	restoreSiteInfrastructure()

	// Ensure all globally configured services have unit files on disk.
	// On macOS this writes launchd plists for any service that has a config
	// entry but no plist (e.g. services installed before the macOS port, or
	// after a clean install from config backup).
	migrateServiceUnits()

	// Start service containers and workers only when autostart is on.
	// When the user has explicitly disabled autostart we leave them
	// stopped — `lerd update` running install via re-exec must not flip
	// disabled units back on.
	if autostartOn {
		startRestoredServices()
	}

	if wantLaravelInstaller {
		fmt.Println("  --> Installing Laravel installer")
		if err := installLaravelInstaller(); err != nil {
			fmt.Printf("    WARN: %v\n", err)
		} else {
			fmt.Println("    OK")
		}
	}

	killTray()
	if services.Mgr.IsEnabled("lerd-tray") {
		_ = services.Mgr.Start("lerd-tray")
	} else {
		if exe, err := os.Executable(); err == nil {
			_ = exec.Command(exe, "tray").Start()
		}
	}

	installAutostart()
	installCleanupScript()

	step("Adding shell PATH configuration")
	if err := addShellShims(); err != nil {
		fmt.Printf("    WARN: %v\n", err)
	}
	ok()

	fmt.Println("\nLerd installation complete!")
	fmt.Println("\n  Dashboard: \033[96mhttp://lerd.localhost\033[0m")
	return nil
}

// ensureSystemdLinger checks whether systemd user linger is enabled for the
// current user and runs `sudo loginctl enable-linger` if not. Without linger
// the rootless Podman containers (lerd-dns, lerd-nginx, PHP-FPM, …) get torn
// down by systemd-logind when the session goes inactive — screen blank,
// lock, switch user, logout — and lerd appears to silently stop working
// until the user manually re-runs `lerd install` or restarts the units.
//
// We only act on a clear "Linger=no" reading. If loginctl is missing or its
// output is unparseable (non-systemd init, container without logind, …) we
// silently skip rather than fail the install.
func ensureSystemdLinger() error {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}
	if user == "" {
		return nil
	}
	if _, err := exec.LookPath("loginctl"); err != nil {
		return nil
	}
	out, err := exec.Command("loginctl", "show-user", user).Output()
	if err != nil {
		return nil
	}
	if !strings.Contains(string(out), "Linger=no") {
		return nil
	}

	fmt.Println("\n  ! systemd user linger is disabled for this account.")
	fmt.Println("    Without it, lerd's containers (DNS, nginx, PHP-FPM) are torn down")
	fmt.Println("    by systemd-logind on screen blank, lock, or logout, and lerd will")
	fmt.Println("    appear to stop working until you manually restart it.")
	fmt.Print("  --> Enabling linger via `sudo loginctl enable-linger ", user, "` ...\n\n")

	cmd := exec.Command("sudo", "loginctl", "enable-linger", user)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Println()
		return fmt.Errorf("enabling linger: %w", err)
	}
	fmt.Println("OK")
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

// downloadBinaries is implemented per-platform in install_linux.go / install_darwin.go.

// laravelInstallerPresent returns true if laravel/installer is already
// installed in the user's composer global vendor directory. The composer
// home is bind-mounted into the FPM container, so the package files live
// on the host and can be detected with a plain stat.
func laravelInstallerPresent() bool {
	composerHome := os.Getenv("COMPOSER_HOME")
	if composerHome == "" {
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(os.Getenv("HOME"), ".config")
		}
		composerHome = filepath.Join(xdgConfig, "composer")
	}
	_, err := os.Stat(filepath.Join(composerHome, "vendor", "laravel", "installer"))
	return err == nil
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
		// Wait for the container to be ready for exec (launchd starts the
		// podman run -d asynchronously, so the container may not exist yet).
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if r, _ := podman.ContainerRunning(container); r {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if r, _ := podman.ContainerRunning(container); !r {
			return fmt.Errorf("%s did not become ready within 30s", container)
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
	cmd := podman.Cmd("exec", "-i",
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
