package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// NewPhpBunCmd returns the php:bun parent command, which manages an optional
// in-container bun runtime. lerd never pins or version-manages bun: install
// pulls the latest musl build into a persistent volume via the bundled npm,
// and `bun upgrade` (run inside `lerd shell`) self-updates from there.
func NewPhpBunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "php:bun",
		Short: "Manage an optional bun runtime inside the PHP-FPM container",
	}
	cmd.AddCommand(newPhpBunInstallCmd())
	cmd.AddCommand(newPhpBunUpdateCmd())
	cmd.AddCommand(newPhpBunVersionCmd())
	return cmd
}

func newPhpBunUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update [php-version]",
		Short: "Update the container's bun in place (bun upgrade)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			version, err := phpExtVersion(args)
			if err != nil {
				return err
			}
			container := fpmContainerName(version)
			if running, _ := podman.ContainerRunning(container); !running {
				return fmt.Errorf("PHP %s FPM container is not running — start it with: %s", version, serviceStartHint(container))
			}
			if !bunInstalledInContainer(version) {
				return fmt.Errorf("bun is not installed in the PHP %s container — run: lerd php:bun install", version)
			}
			fmt.Printf("Updating bun in the PHP %s container...\n", version)
			up := podman.Cmd("exec", container, "/root/.bun/bin/bun", "upgrade")
			up.Stdout = os.Stdout
			up.Stderr = os.Stderr
			if err := up.Run(); err != nil {
				return fmt.Errorf("bun upgrade: %w", err)
			}
			out, _ := podman.Cmd("exec", container, "/root/.bun/bin/bun", "--version").CombinedOutput()
			fmt.Printf("bun is now %s in the PHP %s container.\n", strings.TrimSpace(string(out)), version)
			return nil
		},
	}
}

func newPhpBunInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install [php-version]",
		Short: "Install (or update) bun inside the PHP-FPM container",
		Long: "Installs a musl bun into the container's persistent /root/.bun volume using the bundled npm, so it survives image rebuilds and is shared across sites on that PHP version. " +
			"Run `bun upgrade` inside `lerd shell` to update it later; lerd does not pin a version.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := phpExtVersion(args)
			if err != nil {
				return err
			}
			pin, _ := cmd.Flags().GetString("pin")
			return installContainerBun(version, pin, os.Stdout)
		},
	}
	cmd.Flags().String("pin", "", "pin a specific bun version (e.g. 1.1.45) instead of latest")
	return cmd
}

// fpmContainerName returns the FPM container/unit name for a PHP version.
func fpmContainerName(version string) string {
	return "lerd-php" + strings.ReplaceAll(version, ".", "") + "-fpm"
}

// bunInstalledInContainer reports whether a working bun already lives in the
// version's FPM container volume.
func bunInstalledInContainer(version string) bool {
	container := fpmContainerName(version)
	return podman.Cmd("exec", container, "/root/.bun/bin/bun", "--version").Run() == nil
}

// bunVolumeMounted reports whether the persistent /root/.bun volume is already
// mounted in the running container, so we only restart it when the mount is
// genuinely missing (e.g. a container created before this feature shipped).
func bunVolumeMounted(container string) bool {
	return podman.Cmd("exec", container, "sh", "-c", "grep -qF ' /root/.bun ' /proc/mounts").Run() == nil
}

// installContainerBun installs (or reinstalls) a musl bun into the version's
// FPM container, using the bundled npm so npm's platform detection pulls the
// musl build. It only restarts the container when the persistent volume isn't
// mounted yet, so reruns on an already-prepared container are non-disruptive.
func installContainerBun(version, pin string, w io.Writer) error {
	container := fpmContainerName(version)

	if err := podman.WriteFPMQuadlet(version); err != nil {
		return fmt.Errorf("updating FPM quadlet: %w", err)
	}
	if running, _ := podman.ContainerRunning(container); !running {
		return fmt.Errorf("PHP %s FPM container is not running — start it with: %s", version, serviceStartHint(container))
	}
	if !bunVolumeMounted(container) {
		fmt.Fprintf(w, "Preparing PHP %s container for bun...\n", version)
		if err := podman.RestartUnit(container); err != nil {
			return fmt.Errorf("restarting %s: %w", container, err)
		}
		if err := waitContainerRunning(container, 20*time.Second); err != nil {
			return err
		}
	}

	pkg := "bun"
	if pin != "" {
		pkg = "bun@" + pin
	}
	fmt.Fprintf(w, "Installing %s into the PHP %s container (musl build via npm)...\n", pkg, version)
	install := podman.Cmd("exec", container, "npm", "install", "-g", "--prefix", "/root/.bun", pkg)
	install.Stdout = w
	install.Stderr = w
	if err := install.Run(); err != nil {
		return fmt.Errorf("installing bun in container: %w", err)
	}

	out, err := podman.Cmd("exec", container, "/root/.bun/bin/bun", "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("bun did not run in the container after install: %w\n%s", err, out)
	}
	fmt.Fprintf(w, "bun %s installed in PHP %s container. Use it from `lerd shell`; update it with `bun upgrade`.\n", strings.TrimSpace(string(out)), version)
	return nil
}

func newPhpBunVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version [php-version]",
		Short: "Show the bun version installed in the PHP-FPM container",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			version, err := phpExtVersion(args)
			if err != nil {
				return err
			}
			container := fpmContainerName(version)
			if running, _ := podman.ContainerRunning(container); !running {
				return fmt.Errorf("PHP %s FPM container is not running — start it with: %s", version, serviceStartHint(container))
			}
			out, err := podman.Cmd("exec", container, "/root/.bun/bin/bun", "--version").CombinedOutput()
			if err != nil {
				fmt.Printf("bun is not installed in the PHP %s container — run: lerd php:bun install\n", version)
				return nil
			}
			fmt.Printf("bun %s (PHP %s container)\n", strings.TrimSpace(string(out)), version)
			return nil
		},
	}
}

// waitContainerRunning polls until the named container reports running or the
// timeout elapses, so an exec right after a restart doesn't race the container.
func waitContainerRunning(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if running, _ := podman.ContainerRunning(name); running {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s to start", name)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
