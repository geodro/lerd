package cli

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/siteops"
	"github.com/spf13/cobra"
)

// resolveNginxDomain turns an optional [site] arg plus an optional --branch
// into the domain whose custom nginx override to operate on. No branch means
// the site's primary domain; a branch resolves to that worktree's subdomain
// the same way the daemon does (gitpkg.DetectWorktrees).
func resolveNginxDomain(args []string, branch string) (*config.Site, string, error) {
	name, err := resolveSiteName(args)
	if err != nil {
		return nil, "", err
	}
	site, err := config.FindSite(name)
	if err != nil {
		return nil, "", fmt.Errorf("site %q not found — run 'lerd link' first", name)
	}
	domain, err := siteops.WorktreeDomain(site, branch)
	if err != nil {
		return nil, "", err
	}
	return site, domain, nil
}

// NewNginxCmd returns the `lerd nginx` command group for the per-site custom
// nginx override. Saving goes through the same edit service as the web UI, so
// `nginx -t` validation, backups, and the reload all behave identically.
func NewNginxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "nginx",
		Short: "Show, edit, or reset a site's custom nginx override",
		Long: "Manage the per-site nginx override included at the end of a site's\n" +
			"server block (custom.d/{domain}.conf). Pass --branch to target a\n" +
			"worktree's override instead of the main branch's.",
	}
	cmd.AddCommand(newNginxShowCmd(), newNginxEditCmd(), newNginxResetCmd())
	return cmd
}

func newNginxShowCmd() *cobra.Command {
	var branch string
	var pathOnly bool
	cmd := &cobra.Command{
		Use:   "show [site]",
		Short: "Print the custom nginx override (or its file path with --path)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, domain, err := resolveNginxDomain(args, branch)
			if err != nil {
				return err
			}
			if pathOnly {
				fmt.Fprintln(cmd.OutOrStdout(), siteops.CustomNginxPath(domain))
				return nil
			}
			got, err := siteops.ReadCustomNginx(domain)
			if err != nil {
				return err
			}
			if !got.Exists {
				fmt.Fprintf(cmd.ErrOrStderr(), "No saved override for %s yet; showing the template.\n", domain)
			}
			fmt.Fprint(cmd.OutOrStdout(), got.Body)
			return nil
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "Worktree branch to target instead of the main branch")
	cmd.Flags().BoolVar(&pathOnly, "path", false, "Print the override file path and exit")
	return cmd
}

func newNginxEditCmd() *cobra.Command {
	var branch string
	cmd := &cobra.Command{
		Use:   "edit [site]",
		Short: "Open the custom nginx override in $EDITOR, then validate and reload",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, domain, err := resolveNginxDomain(args, branch)
			if err != nil {
				return err
			}
			got, err := siteops.ReadCustomNginx(domain)
			if err != nil {
				return err
			}
			// Edit a temp copy so the live file is untouched until SaveCustomNginx
			// snapshots, backs up, validates, and reloads it atomically.
			tmp, err := os.CreateTemp("", "lerd-nginx-*.conf")
			if err != nil {
				return err
			}
			tmpPath := tmp.Name()
			defer os.Remove(tmpPath)
			if _, err := tmp.WriteString(got.Body); err != nil {
				_ = tmp.Close()
				return err
			}
			if err := tmp.Close(); err != nil {
				return err
			}
			launched, err := launchEditor(tmpPath)
			if err != nil {
				return err
			}
			if !launched {
				fmt.Printf("Override file: %s\n", siteops.CustomNginxPath(domain))
				fmt.Println("Set $EDITOR to edit it automatically.")
				return nil
			}
			edited, err := os.ReadFile(tmpPath)
			if err != nil {
				return err
			}
			if string(edited) == got.Body {
				fmt.Fprintln(cmd.OutOrStdout(), "No changes.")
				return nil
			}
			res, err := siteops.SaveCustomNginx(domain, string(edited), got.Exists)
			if err != nil {
				return err
			}
			if !res.OK {
				if res.ValidationOutput != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), res.ValidationOutput)
				}
				return fmt.Errorf("%s", res.Error)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved %s and reloaded nginx.\n", domain)
			return nil
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "Worktree branch to target instead of the main branch")
	return cmd
}

func newNginxResetCmd() *cobra.Command {
	var branch string
	cmd := &cobra.Command{
		Use:   "reset [site]",
		Short: "Delete the custom nginx override and reload nginx (backups are kept)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, domain, err := resolveNginxDomain(args, branch)
			if err != nil {
				return err
			}
			if err := siteops.ResetCustomNginx(domain); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Reset %s to bundled defaults.\n", domain)
			return nil
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "Worktree branch to target instead of the main branch")
	return cmd
}
