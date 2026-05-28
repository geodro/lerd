package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/serviceops"
	"github.com/spf13/cobra"
)

// launchEditor opens path in the user's editor, preferring $EDITOR, then
// $VISUAL, then the first of nano/vim/vi found on PATH. It returns
// launched=false with no error when no editor is available, so callers can
// fall back to printing the path.
func launchEditor(path string) (launched bool, err error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		for _, e := range []string{"nano", "vim", "vi"} {
			if _, lookErr := exec.LookPath(e); lookErr == nil {
				editor = e
				break
			}
		}
	}
	if editor == "" {
		return false, nil
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return true, fmt.Errorf("editor exited: %w", err)
	}
	return true, nil
}

func newServiceConfigCmd() *cobra.Command {
	var pathOnly bool
	var noRestart bool
	cmd := &cobra.Command{
		Use:   "config <service>",
		Short: "Edit a service's runtime tuning override (e.g. my.cnf for mysql/mariadb)",
		Long: "Open the user-editable tuning override for a service in $EDITOR.\n\n" +
			"Lerd seeds the file once with a commented template and never overwrites it,\n" +
			"so edits survive `lerd service reinstall` and `lerd update`. The override is\n" +
			"mounted after the bundled config, so any value set here wins. The service is\n" +
			"restarted afterward to apply the change.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			svc, err := config.ResolveServiceForTuning(name)
			if err != nil {
				return err
			}
			// Install-presence guard: ResolveServiceForTuning falls back to the
			// in-binary preset for default services, which would otherwise let
			// `service config <name>` seed + regenerate + restart a quadlet for
			// a service the user has explicitly removed — effectively a silent
			// reinstall as a side effect of an edit command. Block that here.
			if !serviceops.ServiceInstalled(name) {
				return fmt.Errorf("service %q is not installed — run `lerd service preset install %s` first", name, name)
			}
			if _, ok := config.ServiceTuningMount(svc); !ok {
				supported := strings.Join(config.TuningFamilies(), ", ")
				if fam := config.FamilyOf(svc); fam != "" {
					return fmt.Errorf("service %q (family %q) does not support tuning yet (supported: %s)", name, fam, supported)
				}
				return fmt.Errorf("service %q does not support tuning yet (supported: %s)", name, supported)
			}
			if err := config.MaterializeServiceTuning(svc); err != nil {
				return fmt.Errorf("creating tuning file: %w", err)
			}

			path := config.ServiceTuningFile(name)
			if pathOnly {
				fmt.Fprintln(cmd.OutOrStdout(), path)
				return nil
			}

			launched, err := launchEditor(path)
			if err != nil {
				return err
			}
			if !launched {
				fmt.Printf("Tuning file: %s\n", path)
				fmt.Println("Set $EDITOR to open it automatically.")
				return nil
			}

			// Quadlet regen and restart go through the shared serviceops
			// helpers so the CLI, the /api/services/{name}/config handler,
			// and any future surface (MCP, …) can't drift from each other.
			// A regen failure here is a hard error: skipping it orphans
			// the just-written override on installs predating the tuning
			// Volume= mount, so the next restart re-reads the OLD config.
			if err := serviceops.EnsureTuningQuadlet(name, svc); err != nil {
				return err
			}
			if noRestart {
				fmt.Printf("Saved %s. Run `lerd service restart %s` to apply.\n", path, name)
				return nil
			}
			unit := "lerd-" + name
			fmt.Printf("Saved. Restarting %s...\n", unit)
			if err := podman.RestartUnit(unit); err != nil {
				return fmt.Errorf("restarting %s: %w", unit, err)
			}
			fmt.Println("Done.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&pathOnly, "path", false, "Print the tuning file path and exit (no editor, no restart)")
	cmd.Flags().BoolVar(&noRestart, "no-restart", false, "Edit without restarting the service afterward")
	return cmd
}
