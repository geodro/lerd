package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	"github.com/spf13/cobra"
)

// NewAutostartCmd returns the autostart command with enable/disable subcommands.
//
// "Autostart" governs whether lerd comes up at login as a single switch:
// every lerd-* container quadlet, the lerd UI, the watcher, and every
// per-site worker/queue/schedule/horizon/reverb/stripe unit are enabled
// or disabled together. The state lives in cfg.Autostart.Disabled (zero
// value = enabled, so existing installs are unchanged) and is the
// canonical source of truth — `IsAutostartEnabled` reads it directly.
//
// The toggle only affects what happens at the next login. Toggling to
// off does NOT stop currently-running services, and toggling to on does
// NOT start anything — the user is in the middle of working and a
// session-level switch should not yank infrastructure out from under
// them. Disabling does two things:
//
//  1. Strips the [Install] section from every lerd-*.container quadlet
//     on disk so the podman-system-generator stops emitting the
//     `default.target.wants/<name>.service` symlink on the next
//     daemon-reload. This is the only way to actually stop a quadlet
//     from auto-starting; `systemctl --user disable` is a no-op for
//     generator units.
//  2. Runs `systemctl --user disable` on every lerd-*.service file
//     (lerd-ui, lerd-watcher, every per-site worker/queue/schedule/
//     horizon/reverb/stripe), removing the `default.target.wants`
//     symlink without touching the running unit.
//
// Enabling reverses both steps. To actually stop or start a running
// environment use `lerd stop` / `lerd start` — those are the live-state
// commands.
func NewAutostartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autostart",
		Short: "Manage autostart on login",
	}
	cmd.AddCommand(newAutostartEnableCmd())
	cmd.AddCommand(newAutostartDisableCmd())
	return cmd
}

func newAutostartEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable lerd autostart on login",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := ApplyAutostart(false); err != nil {
				return fmt.Errorf("enabling autostart: %w", err)
			}
			fmt.Println("Autostart enabled — lerd will start automatically on login.")
			return nil
		},
	}
}

func newAutostartDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable lerd autostart on login",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := ApplyAutostart(true); err != nil {
				return fmt.Errorf("disabling autostart: %w", err)
			}
			fmt.Println("Autostart disabled — lerd will not start automatically on login.")
			return nil
		},
	}
}

// quadletInstallBlock is the [Install] stanza appended to every lerd-*.container
// quadlet when autostart is enabled. Identical to what every embedded quadlet
// ships with, so a strip-then-restore round-trip yields the same on-disk file
// the install pass would have written.
const quadletInstallBlock = "[Install]\nWantedBy=default.target\n"

// ApplyAutostart writes the new flag to config.yaml, rewrites every
// lerd-*.container quadlet on disk so its [Install] section is present
// (when enabled) or absent (when disabled), runs `systemctl --user
// daemon-reload`, then enables/disables (and starts/stops) every lerd-*
// systemd user service. Safe to call when the flag is already in the
// requested state — every step is idempotent.
//
// Exposed (capitalised) so the UI server can call it directly without
// duplicating the orchestration; the tray and CLI go through the same
// path.
func ApplyAutostart(disabled bool) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	cfg.Autostart.Disabled = disabled
	if err := config.SaveGlobal(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if err := rewriteQuadletsForAutostart(disabled); err != nil {
		return fmt.Errorf("rewriting quadlets: %w", err)
	}
	_ = services.Mgr.DaemonReload()

	units := lerdSystemd.AutostartUserUnits()
	for _, u := range units {
		name := strings.TrimSuffix(u, ".service")
		if disabled {
			_ = services.Mgr.Disable(name)
		} else {
			_ = services.Mgr.Enable(name)
		}
	}
	return nil
}

// rewriteQuadletsForAutostart walks every lerd-*.container in the user's
// quadlet directory and rewrites it in place so the [Install] section is
// stripped (disabled=true) or restored (disabled=false). The on-disk
// content is the source of truth so this works equally for embedded
// quadlets, dynamically-generated FPM quadlets, and custom service
// quadlets — there is no need to know how each one was originally
// produced.
func rewriteQuadletsForAutostart(disabled bool) error {
	for _, path := range listLerdQuadletFiles() {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Strip any existing [Install] block, then restore it (or not)
		// based on the new state. StripInstallSection with true does
		// the strip; passing false is a no-op, so we always strip
		// first to normalize.
		stripped := podman.StripInstallSection(string(raw), true)
		var out string
		if disabled {
			out = stripped
		} else {
			// Re-add the canonical [Install] block. Trim a trailing
			// run of blanks first so the file ends with exactly one
			// blank line before [Install].
			trimmed := strings.TrimRight(stripped, "\n") + "\n\n"
			out = trimmed + quadletInstallBlock
		}
		if string(raw) == out {
			continue
		}
		if err := os.WriteFile(path, []byte(out), 0644); err != nil {
			return err
		}
	}
	return nil
}

// listLerdQuadletFiles returns absolute paths of every lerd-*.container
// in the user's quadlet directory.
func listLerdQuadletFiles() []string {
	entries, _ := filepath.Glob(filepath.Join(config.QuadletDir(), "lerd-*.container"))
	return entries
}

// listLerdQuadlets returns the generated unit names (lerd-<name>) for
// every quadlet currently installed.
func listLerdQuadlets() []string {
	files := listLerdQuadletFiles()
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, strings.TrimSuffix(filepath.Base(f), ".container"))
	}
	return out
}
