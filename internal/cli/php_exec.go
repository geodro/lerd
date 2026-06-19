package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/agentenv"
	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewPhpCmd returns the php command — runs PHP in the appropriate FPM container.
func NewPhpCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "php [args...]",
		Short:              "Run PHP in the project's container (e.g. lerd php artisan migrate)",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		RunE:               runPhp,
	}
}

func runPhp(_ *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return RunPHP(cwd, args)
}

// RunPHP execs `php <args...>` inside the project's PHP-FPM container, with
// stdio wired to the current terminal. Used by `lerd php`, the vendor/bin
// fallback, and other passthrough commands that need a PHP runtime. The
// child's exit code is propagated via os.Exit; callers that need to do work
// after the child exits (e.g. sync wrappers after a failed composer remove)
// should use RunPHPCapture instead.
func RunPHP(cwd string, args []string) error {
	code, err := RunPHPCapture(cwd, args)
	if err != nil {
		return err
	}
	if code != 0 {
		os.Exit(code)
	}
	return nil
}

// RunPHPCapture is the non-exiting variant of RunPHP. It returns the child
// process's exit code separately from any setup error (container not running,
// version detection failure, etc.), so callers can run their own work after
// the child exits before propagating the code to the parent shell.
func RunPHPCapture(cwd string, args []string) (int, error) {
	return RunPHPCaptureEnv(cwd, args, nil)
}

// fpmContainerForDir resolves the FPM container an exec in dir should target:
// the per-site container for custom-FPM sites, otherwise the shared
// lerd-php<version>-fpm container. dir may be a subdirectory of the project, so
// it resolves the containing site root first (matching version detection, which
// also walks up), otherwise a custom-FPM exec from a subdir would wrongly hit
// the shared container.
func fpmContainerForDir(dir, version string) string {
	if site, _ := config.FindSiteByPath(siteRootFor(dir)); site != nil {
		return podman.FPMContainerName(*site, version)
	}
	return "lerd-php" + strings.ReplaceAll(version, ".", "") + "-fpm"
}

// RunPHPCaptureEnv is RunPHPCapture with extra KEY=VALUE environment entries
// injected into the container exec — used by `lerd profile run` to set
// SPX_ENABLED so a CLI command is profiled.
func RunPHPCaptureEnv(cwd string, args []string, extraEnv []string) (int, error) {
	recordCwdActivity(cwd) // keep the site awake under idle-suspend while you work in the terminal
	version, err := phpDet.DetectVersion(cwd)
	if err != nil {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			return 0, fmt.Errorf("cannot detect PHP version: %w", err)
		}
		version = cfg.PHP.DefaultVersion
	}

	container := fpmContainerForDir(cwd, version)

	home := os.Getenv("HOME")
	composerHome := os.Getenv("COMPOSER_HOME")
	if composerHome == "" {
		// Respect XDG: prefer ~/.config/composer, fall back to ~/.composer
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(home, ".config")
		}
		composerHome = filepath.Join(xdgConfig, "composer")
	}
	composerBin := filepath.Join(composerHome, "vendor", "bin")
	projectVendorBin := filepath.Join(cwd, "vendor", "bin")

	if running, _ := podman.ContainerRunning(container); !running {
		return 0, fmt.Errorf("PHP %s FPM container is not running — start it with: systemctl --user start %s", version, container)
	}

	podman.EnsurePathMounted(cwd, version)
	ensureServicesForCwd(cwd)

	// If any positional arg is an absolute path to a file that exists on the
	// host but outside $HOME (e.g. /tmp/ide-phpinfo.php written by PhpStorm),
	// the container won't be able to read it since only $HOME is volume-mounted.
	// Stream the file through stdin and replace the arg with /dev/stdin.
	var stdinReader io.Reader = os.Stdin
	useTTY := term.IsTerminal(int(os.Stdin.Fd()))
	for i, arg := range args {
		if filepath.IsAbs(arg) && !strings.HasPrefix(arg, home+"/") && arg != home {
			if data, err := os.ReadFile(arg); err == nil {
				args[i] = "/dev/stdin"
				stdinReader = bytes.NewReader(data)
				useTTY = false
				break
			}
		}
	}

	execFlags := []string{"exec", "-i"}
	if useTTY {
		execFlags = append(execFlags, "-t")
	}

	cmdArgs := append(execFlags, "-w", cwd,
		"--env", "HOME="+home,
		"--env", "COMPOSER_HOME="+composerHome,
		"--env", "PATH="+projectVendorBin+":/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:"+composerBin,
	)
	// Forward SPX_* profiler vars from the host so `SPX_ENABLED=1 php ...` (or
	// any shim'd tool like composer) reaches SPX inside the container. extraEnv
	// is applied after, so an explicit caller like `lerd profile run` wins.
	for _, e := range spxPassthroughEnv(os.Environ()) {
		cmdArgs = append(cmdArgs, "--env", e)
	}
	// Forward AI agent detection vars so agent-detector (e.g. laravel/pao)
	// still emits JSON when run inside the container.
	for _, e := range agentenv.Passthrough(os.Environ()) {
		cmdArgs = append(cmdArgs, "--env", e)
	}
	for _, e := range extraEnv {
		cmdArgs = append(cmdArgs, "--env", e)
	}
	cmdArgs = append(cmdArgs, container, "php")
	cmdArgs = append(cmdArgs, args...)

	cmd := podman.Cmd(cmdArgs...)
	cmd.Stdin = stdinReader
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), nil
		}
		return 0, err
	}
	return 0, nil
}

// spxPassthroughEnv picks the SPX_* profiler vars out of environ. When SPX is
// enabled but no report type is set, it defaults SPX_REPORT to full so the run
// lands in the Profiler view instead of a terminal flat profile.
func spxPassthroughEnv(environ []string) []string {
	var out []string
	enabled, hasReport := false, false
	for _, e := range environ {
		if !strings.HasPrefix(e, "SPX_") {
			continue
		}
		out = append(out, e)
		k, v, _ := strings.Cut(e, "=")
		switch k {
		case "SPX_ENABLED":
			enabled = v != "" && v != "0"
		case "SPX_REPORT":
			hasReport = true
		}
	}
	if enabled && !hasReport {
		out = append(out, "SPX_REPORT=full")
	}
	return out
}
