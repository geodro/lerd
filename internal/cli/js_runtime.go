package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/geodro/lerd/internal/config"
	nodeDet "github.com/geodro/lerd/internal/node"
	"github.com/spf13/cobra"
)

// NewJSRuntimeCmd returns the js:runtime command, the CLI counterpart to the
// dashboard's bun/Node toggle. It pins js_runtime in the current site's
// .lerd.yaml and re-syncs the host workers so the dev/Vite worker switches
// runtime immediately, exactly like the UI does. With no argument it prints the
// current setting and what it resolves to.
func NewJSRuntimeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "js:runtime [bun|node|auto]",
		Short: "Pin the JS runtime (bun or Node) for the current site",
		Long: "Pins the JavaScript runtime for the current site in .lerd.yaml, the CLI equivalent of the dashboard's bun/Node toggle. " +
			"`bun` forces bun, `node` forces Node/npm (opting out of bun auto-detection), and `auto` clears the pin so lerd detects bun from a lockfile or the no-Node fallback. " +
			"Run with no argument to show the current setting.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			site, err := resolveSiteForCwd()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return showJSRuntime(*site, os.Stdout)
			}
			return setJSRuntime(*site, args[0], os.Stdout)
		},
	}
}

// setJSRuntime maps the user's choice to a js_runtime value, writes it, and
// regenerates the site's host workers so a running Vite/dev worker switches
// runtime without a manual restart. Mirrors the UI's bun-toggle handler.
func setJSRuntime(site config.Site, choice string, w io.Writer) error {
	value, label, ok := jsRuntimeValue(choice)
	if !ok {
		return fmt.Errorf("unknown runtime %q — use bun, node, or auto", choice)
	}
	if err := config.SetProjectJSRuntime(site.Path, value); err != nil {
		return fmt.Errorf("setting js_runtime: %w", err)
	}
	RegenerateHostWorkersForSite(site)
	fmt.Fprintf(w, "JS runtime for %s set to %s.\n", site.Name, label)
	if value == "bun" && nodeDet.BunPath() == "" {
		fmt.Fprintln(w, "Note: bun isn't installed on the host yet — install it with `curl -fsSL https://bun.sh/install | bash`.")
	}
	return nil
}

// jsRuntimeValue normalizes a CLI choice to the js_runtime field value and a
// human label. "auto" (and "") clear the pin; npm is accepted as a Node alias.
func jsRuntimeValue(choice string) (value, label string, ok bool) {
	switch choice {
	case "bun":
		return "bun", "bun", true
	case "node", "npm":
		return "node", "Node", true
	case "auto", "":
		return "", "auto-detect", true
	default:
		return "", "", false
	}
}

// showJSRuntime prints the pinned runtime, or "auto-detect" plus the runtime
// lerd currently resolves to, so the user can see the effective choice.
func showJSRuntime(site config.Site, w io.Writer) error {
	switch nodeDet.JSRuntime(site.Path) {
	case "bun":
		fmt.Fprintf(w, "%s is pinned to bun.\n", site.Name)
	case "node":
		fmt.Fprintf(w, "%s is pinned to Node.\n", site.Name)
	default:
		resolved := "Node"
		if nodeDet.UsesBun(site.Path) {
			resolved = "bun"
		}
		fmt.Fprintf(w, "%s uses auto-detect (currently %s). Pin it with `lerd js:runtime bun|node`.\n", site.Name, resolved)
	}
	return nil
}
