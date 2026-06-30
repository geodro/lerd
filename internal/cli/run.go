package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/feedback"
	"github.com/spf13/cobra"
)

// NewRunCmd returns the `lerd run` command, the CLI alias for the framework
// commands feature. With no args it lists available commands for the current
// site; with a name it executes that command in the project directory.
func NewRunCmd() *cobra.Command {
	var assumeYes bool
	cmd := &cobra.Command{
		Use:   "run [name]",
		Short: "Run a framework command (artisan optimize:clear, drush cr, etc.) in the current site",
		Long: `Run a command defined by the site's framework or its .lerd.yaml.

With no arguments, lists all commands available in the current project.
With a command name, executes that command in the project's directory and
streams output to your terminal.

Commands marked confirm: true prompt before running unless --yes is set.`,
		Args: cobra.MaximumNArgs(1),
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			cwd, err := os.Getwd()
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			cmds := resolveCommandsForCwd(cwd)
			names := make([]string, 0, len(cmds))
			for _, c := range cmds {
				names = append(names, c.Name)
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cmds := resolveCommandsForCwd(cwd)
			if len(args) == 0 {
				return listCommands(cmds)
			}
			return runNamedCommand(cwd, cmds, args[0], assumeYes)
		},
	}
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "Skip confirmation prompts on destructive commands")
	return cmd
}

// projectRootFromCwd walks up from cwd looking for the nearest .lerd.yaml.
// Returns cwd unchanged if nothing is found (lets callers handle the
// "no project" case downstream). Stops at the filesystem root.
func projectRootFromCwd(cwd string) string {
	d := cwd
	for {
		if _, err := os.Stat(filepath.Join(d, ".lerd.yaml")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return cwd
		}
		d = parent
	}
}

func resolveCommandsForCwd(cwd string) []config.FrameworkCommand {
	root := projectRootFromCwd(cwd)
	proj, _ := config.LoadProjectConfig(root)
	var fw *config.Framework
	if proj != nil && proj.Framework != "" {
		fw, _ = config.GetFrameworkForDir(proj.Framework, root)
	}
	if fw == nil {
		// Fall back to detection in case .lerd.yaml is absent.
		if name, ok := config.DetectFramework(root); ok {
			fw, _ = config.GetFrameworkForDir(name, root)
		}
	}
	return config.ResolveCommands(fw, proj, root)
}

func listCommands(cmds []config.FrameworkCommand) error {
	if len(cmds) == 0 {
		feedback.Begin()
		feedback.Line("no commands available for this project")
		feedback.Note("add a commands: block to .lerd.yaml or install the framework store")
		return nil
	}
	maxName := 0
	for _, c := range cmds {
		if len(c.Name) > maxName {
			maxName = len(c.Name)
		}
	}
	for _, c := range cmds {
		marker := " "
		if c.Confirm {
			marker = "*"
		}
		desc := c.Description
		if desc == "" {
			desc = c.Label
		}
		fmt.Printf("  %s %-*s  %s\n", marker, maxName, c.Name, desc)
	}
	fmt.Println()
	fmt.Println("  * = asks for confirmation. Use --yes to skip.")
	return nil
}

func runNamedCommand(cwd string, cmds []config.FrameworkCommand, name string, assumeYes bool) error {
	var target *config.FrameworkCommand
	for i := range cmds {
		if cmds[i].Name == name {
			target = &cmds[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("command %q not found. Run `lerd run` to list available commands", name)
	}
	if target.Command == "" {
		return fmt.Errorf("command %q has no shell invocation", name)
	}

	// A command from the project's untrusted .lerd.yaml (top-level commands or an
	// embedded framework_def) runs on the host, so require consent the first time;
	// trusted framework commands are unaffected. --yes bypasses the gate.
	if target.ProjectOrigin && !assumeYes {
		siteName := ""
		if site, _ := config.FindSiteByPath(projectRootFromCwd(cwd)); site != nil {
			siteName = site.Name
		}
		if err := approveHostCommand(siteName, target.Command, fmt.Sprintf("command %q", name)); err != nil {
			return err
		}
	}

	if target.Confirm && !assumeYes {
		if !feedback.Confirm("This will run: "+target.Command+"\nContinue?", false) {
			return fmt.Errorf("aborted")
		}
	}

	runDir := projectRootFromCwd(cwd)
	if target.CWD != "" && target.CWD != "." {
		runDir = filepath.Join(runDir, target.CWD)
	}

	c := exec.Command("sh", "-c", target.Command)
	c.Dir = runDir
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		return err
	}
	return nil
}
