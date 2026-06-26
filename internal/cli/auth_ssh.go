package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewAuthCmd returns the `auth` command group: sharing host credentials with the
// project containers.
func NewAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Share host credentials (SSH keys) with the project containers",
	}
	cmd.AddCommand(newAuthSSHCmd())
	return cmd
}

func newAuthSSHCmd() *cobra.Command {
	var list, remove bool
	c := &cobra.Command{
		Use:   "ssh [key...]",
		Short: "Load SSH keys into a shared agent so composer can use git over SSH",
		Long: "Start a shared ssh-agent container and load your SSH keys into it, so " +
			"`lerd composer` can authenticate to private git repositories with " +
			"passphrase-protected keys. With no arguments it loads ~/.ssh/id_* ; pass " +
			"key paths (under ~/.ssh) to load specific keys. Keys live only in the " +
			"agent's memory and are cleared when it stops or the machine restarts.",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			switch {
			case remove:
				return authSSHRemove()
			case list:
				return authSSHList()
			default:
				return authSSHAdd(args)
			}
		},
	}
	c.Flags().BoolVar(&list, "list", false, "List the keys currently loaded in the agent")
	c.Flags().BoolVar(&remove, "remove", false, "Flush all keys and stop the agent")
	return c
}

// authSSHAdd ensures the agent sidecar is up and loads the requested keys.
func authSSHAdd(keys []string) error {
	resolved, err := resolveSSHKeys(keys)
	if err != nil {
		return err
	}
	if len(resolved) == 0 {
		return fmt.Errorf("no SSH keys found in ~/.ssh (looked for id_*); pass a key path explicitly, e.g. lerd auth ssh ~/.ssh/mykey")
	}
	if err := ensureSSHAgent(); err != nil {
		return err
	}

	// Allocate a TTY only when we have one, so a passphrase prompt works
	// interactively while a passphraseless add (or a scripted run) still works
	// without a terminal.
	execArgs := []string{"exec", "-i"}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		execArgs = append(execArgs, "-t")
	}
	execArgs = append(execArgs, "--env", "SSH_AUTH_SOCK="+podman.SSHAgentSocket,
		podman.SSHAgentContainer, "ssh-add")
	execArgs = append(execArgs, resolved...)
	cmd := podman.Cmd(execArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh-add: %w", err)
	}
	fmt.Println("Keys loaded. `lerd composer` can now use git over SSH for private packages.")
	return nil
}

// authSSHList prints the keys currently held by the agent.
func authSSHList() error {
	if !podman.ContainerRunningQuiet(podman.SSHAgentContainer) {
		return fmt.Errorf("ssh-agent is not running; run `lerd auth ssh` first")
	}
	cmd := podman.Cmd("exec", "--env", "SSH_AUTH_SOCK="+podman.SSHAgentSocket,
		podman.SSHAgentContainer, "ssh-add", "-l")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// authSSHRemove flushes the loaded keys and stops the agent sidecar.
func authSSHRemove() error {
	if podman.ContainerRunningQuiet(podman.SSHAgentContainer) {
		flush := podman.Cmd("exec", "--env", "SSH_AUTH_SOCK="+podman.SSHAgentSocket,
			podman.SSHAgentContainer, "ssh-add", "-D")
		flush.Stdout, flush.Stderr = os.Stdout, os.Stderr
		_ = flush.Run()
	}
	if err := podman.StopUnit(podman.SSHAgentUnit); err != nil {
		return fmt.Errorf("stopping ssh-agent: %w", err)
	}
	fmt.Println("SSH agent stopped and keys flushed.")
	return nil
}

// ensureSSHAgent makes the FPM containers mount the agent volume, writes and
// starts the agent sidecar quadlet, and waits until the socket is ready.
func ensureSSHAgent() error {
	// Regenerate FPM quadlets so they mount the agent volume, restarting any
	// whose unit changed. No-op once the mount is already in place.
	if err := podman.RewriteFPMQuadlets(); err != nil {
		return fmt.Errorf("updating FPM containers for the agent mount: %w", err)
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	version := cfg.PHP.DefaultVersion
	image := fmt.Sprintf("lerd-php%s-fpm:local", strings.ReplaceAll(version, ".", ""))

	changed, err := podman.WriteQuadletDiff(podman.SSHAgentUnit, podman.GenerateSSHAgentQuadlet(image))
	if err != nil {
		return fmt.Errorf("writing ssh-agent unit: %w", err)
	}
	if err := podman.DaemonReloadIfNeeded(changed); err != nil {
		return err
	}
	if err := podman.StartUnit(podman.SSHAgentUnit); err != nil {
		return fmt.Errorf("starting ssh-agent (is the PHP %s image built? try `lerd php rebuild`): %w", version, err)
	}

	// Give the sidecar a moment to come up and create the socket before ssh-add.
	for i := 0; i < 50; i++ {
		if podman.ContainerRunningQuiet(podman.SSHAgentContainer) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("ssh-agent container did not start in time")
}

// resolveSSHKeys turns the user's key arguments (or the ~/.ssh/id_* default)
// into absolute paths under ~/.ssh. Keys must live under ~/.ssh because that is
// the only host directory mounted into the agent container.
func resolveSSHKeys(keys []string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	sshDir := filepath.Join(home, ".ssh")

	if len(keys) > 0 {
		var out []string
		for _, k := range keys {
			if !filepath.IsAbs(k) {
				k = filepath.Join(sshDir, k)
			}
			abs, err := filepath.Abs(k)
			if err != nil {
				return nil, err
			}
			if rel, err := filepath.Rel(sshDir, abs); err != nil || strings.HasPrefix(rel, "..") {
				return nil, fmt.Errorf("key must be under ~/.ssh (the only directory shared with the agent): %s", k)
			}
			if _, err := os.Stat(abs); err != nil {
				return nil, fmt.Errorf("key not found: %s", abs)
			}
			out = append(out, abs)
		}
		return out, nil
	}

	// Default: every ~/.ssh/id_* private key (skip the .pub counterparts).
	matches, _ := filepath.Glob(filepath.Join(sshDir, "id_*"))
	var out []string
	for _, m := range matches {
		if strings.HasSuffix(m, ".pub") {
			continue
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}
