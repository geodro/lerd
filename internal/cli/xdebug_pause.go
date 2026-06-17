package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/logsource"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

// `lerd xdebug pause` drives Xdebug's control socket (Xdebug >= 3.3) to break
// the IDE debugger into an already-running PHP process — a queue/Horizon worker
// or a CLI script — without a trigger cookie or per-request connection attempts.
// It shells out to the upstream `xdebugctl` tool, which is baked into the FPM
// image (see lerd-php-fpm.Containerfile); FrankenPHP and custom-container sites
// run their own image without it, so the command is PHP-FPM only.
const xdebugctlInContainer = "/usr/local/bin/xdebugctl"

func newXdebugPauseCmd() *cobra.Command {
	var pid int
	var list bool
	cmd := &cobra.Command{
		Use:   "pause [site]",
		Short: "(experimental) Break the IDE debugger into a running PHP process via Xdebug's control socket",
		Long: "Use Xdebug's control socket to make a running PHP process (a queue/Horizon worker or a\n" +
			"CLI script) connect to your IDE and break in — no trigger cookie, no per-request\n" +
			"overhead. PHP-FPM sites only: FrankenPHP and custom-container sites run their own image\n" +
			"without xdebugctl. Requires Xdebug debug mode enabled for the site's PHP version\n" +
			"(`lerd xdebug on`) and your IDE listening on port 9003.\n\n" +
			"Run with --list to see the candidate processes, then --pid to target one.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runXdebugPause(args, pid, list)
		},
	}
	cmd.Flags().IntVarP(&pid, "pid", "p", 0, "target a specific PHP process id (from --list)")
	cmd.Flags().BoolVar(&list, "list", false, "list PHP processes that expose an Xdebug control socket")
	return cmd
}

func runXdebugPause(args []string, pid int, list bool) error {
	site, err := resolvePauseSite(args)
	if err != nil {
		return err
	}
	// xdebugctl is baked into the shared FPM image (which custom-FPM sites build
	// FROM); FrankenPHP and custom-container images don't ship it.
	if site.IsFrankenPHP() || site.IsCustomContainer() {
		return fmt.Errorf("`lerd xdebug pause` is only available on PHP-FPM sites; %q runs its own container without xdebugctl", site.Name)
	}
	version := pauseSiteVersion(site)
	if version == "" {
		return fmt.Errorf("could not determine the PHP version for %q — set it with `lerd isolate <version>` first", site.Name)
	}
	container := resolveWorkerFPMUnit(site.Name, version)
	if container == "" {
		return fmt.Errorf("site %q runs no container to attach to", site.Name)
	}

	// Pausing needs debug mode actually loaded; listing is harmless either way
	// (xdebugctl ps simply shows nothing when no process has a control socket).
	if !list {
		if cfg, err := config.LoadGlobal(); err == nil && !strings.Contains(cfg.GetXdebugMode(version), "debug") {
			return fmt.Errorf("Xdebug debug mode is not enabled for PHP %s — run: lerd xdebug on %s", version, version)
		}
	}

	ctl, err := ensureXdebugctl(container)
	if err != nil {
		return err
	}

	if pid == 0 {
		out, _ := podman.Cmd("exec", container, ctl, "ps").CombinedOutput()
		fmt.Print(string(out))
		if list {
			return nil
		}
		// The shared FPM container lists workers from every site; scope the
		// auto-target to this site's own processes by project path.
		scoped := procsForSite(parseXdebugctlProcs(string(out)), site.Path)
		switch len(scoped) {
		case 0:
			return fmt.Errorf("no PHP process for %q with an Xdebug control socket in %s — start the worker/script after enabling Xdebug, or pass --pid", site.Name, container)
		case 1:
			pid = scoped[0].pid
		default:
			return fmt.Errorf("multiple processes for %q; re-run with --pid <PID> from the list above", site.Name)
		}
	}

	out, err := podman.Cmd("exec", container, ctl, "-p", strconv.Itoa(pid), "pause").CombinedOutput()
	if s := strings.TrimSpace(string(out)); s != "" {
		fmt.Println(s)
	}
	if err != nil {
		return fmt.Errorf("xdebugctl pause: %w", err)
	}
	fmt.Printf("Sent pause to PID %d in %s — your IDE (listening on :9003) should break in.\n", pid, container)
	if cfg, err := config.LoadGlobal(); err == nil && cfg.GetXdebugStart(version) == "yes" {
		fmt.Println("Tip: `lerd xdebug on --on-demand` stops every other request/worker from also connecting to your IDE.")
	}
	return nil
}

func resolvePauseSite(args []string) (*config.Site, error) {
	if len(args) == 1 {
		return config.FindSite(args[0])
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return nil, fmt.Errorf("no lerd site here — run inside a project or pass a site name")
	}
	return site, nil
}

func pauseSiteVersion(site *config.Site) string {
	if d, err := phpDet.DetectVersion(site.Path); err == nil && d != "" {
		return d
	}
	if site.PHPVersion != "" {
		return site.PHPVersion
	}
	if cfg, err := config.LoadGlobal(); err == nil {
		return cfg.PHP.DefaultVersion
	}
	return ""
}

type xdebugProc struct {
	pid     int
	command string
}

// parseXdebugctlProcs parses the data rows of `xdebugctl ps` output (PID, RSS,
// TIME, COMMAND), skipping the header and any "Error: No response" rows for
// idle processes that didn't answer the control socket in time.
func parseXdebugctlProcs(out string) []xdebugProc {
	// xdebugctl colours its output unconditionally; strip the escapes so the
	// PID is the first field and project paths match cleanly.
	out = logsource.StripANSI(out)
	var procs []xdebugProc
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[1] == "Error:" {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		procs = append(procs, xdebugProc{pid: pid, command: strings.Join(fields[3:], " ")})
	}
	return procs
}

// procsForSite keeps the processes whose command path lives under the site's
// project directory, so a bare `pause <site>` targets that site's own workers
// rather than another site's that happen to share the FPM container.
func procsForSite(procs []xdebugProc, sitePath string) []xdebugProc {
	if sitePath == "" {
		return procs
	}
	prefix := strings.TrimRight(sitePath, "/") + "/"
	var out []xdebugProc
	for _, p := range procs {
		// The command begins with the script path; match on a path boundary so
		// /proj/app doesn't also capture /proj/app2's workers.
		if strings.HasPrefix(p.command, prefix) {
			out = append(out, p)
		}
	}
	return out
}

// ensureXdebugctl confirms the baked-in xdebugctl binary is present in the
// container. It ships in the FPM image; an older image won't have it.
func ensureXdebugctl(container string) (string, error) {
	// Distinguish a stopped container from a genuinely missing binary: exec fails
	// either way, but the fix differs (start the site vs rebuild the image).
	if running, _ := podman.ContainerRunning(container); !running {
		return "", fmt.Errorf("container %s is not running — start the site (and its worker or CLI script) first, then retry", container)
	}
	if podman.Cmd("exec", container, "test", "-x", xdebugctlInContainer).Run() == nil {
		return xdebugctlInContainer, nil
	}
	return "", fmt.Errorf("xdebugctl is not in %s — rebuild the PHP image with `lerd php:rebuild` to bake it in", container)
}
