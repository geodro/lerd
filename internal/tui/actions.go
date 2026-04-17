package tui

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteinfo"
)

// ActionResult is emitted when an async action (start/stop/restart) finishes.
// The status bar renders Err/Detail so the user sees failures without
// dropping out of the TUI.
type ActionResult struct {
	Summary string
	Err     error
	Detail  string
}

// runShellIn suspends bubbletea, opens an interactive shell inside the
// chosen container (FPM for PHP sites, the custom container for Node/Go/etc.
// sites, and the service's own container for services), then resumes. Uses
// `sh -c 'command -v bash && exec bash || exec sh'` so bash is used when
// available and sh is the fallback (matters for minimal service images like
// alpine where bash isn't installed).
func runShellIn(container, workDir string) tea.Cmd {
	args := []string{"exec", "-it"}
	if workDir != "" {
		args = append(args, "-w", workDir)
	}
	args = append(args, container, "sh", "-c",
		"command -v bash >/dev/null 2>&1 && exec bash || exec sh")
	cmd := exec.Command(podman.PodmanBin(), args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return ActionResult{Summary: "shell " + container, Err: err, Detail: err.Error()}
		}
		return ActionResult{Summary: "shell " + container + " exited"}
	})
}

// containerForSite picks the right container to exec into for a site: the
// custom container if the site is a non-PHP project, otherwise the shared
// FPM image for that site's PHP version. Returns "" if neither exists yet
// (e.g. PHP version hasn't been detected).
func containerForSite(s *siteinfo.EnrichedSite) string {
	if s.ContainerPort > 0 {
		return podman.CustomContainerName(s.Name)
	}
	if s.PHPVersion == "" {
		return ""
	}
	return "lerd-php" + strings.ReplaceAll(s.PHPVersion, ".", "") + "-fpm"
}

// runLerd executes `lerd` as a subprocess with the given args, in the given
// working directory. We go through the public CLI rather than poking internal
// helpers so the TUI goes down the same code paths users would run manually.
// This avoids drifting when ensureQuadlet / dependency logic moves around.
func runLerd(dir string, args ...string) tea.Cmd {
	return func() tea.Msg {
		self, err := os.Executable()
		if err != nil {
			self = "lerd"
		}
		cmd := exec.Command(self, args...)
		if dir != "" {
			cmd.Dir = dir
		}
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		runErr := cmd.Run()
		return ActionResult{
			Summary: "lerd " + strings.Join(args, " "),
			Err:     runErr,
			Detail:  strings.TrimSpace(buf.String()),
		}
	}
}
