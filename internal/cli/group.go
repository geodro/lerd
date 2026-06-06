package cli

import (
	"fmt"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/grouping"
	"github.com/spf13/cobra"
)

// NewGroupCmd returns the group command for managing site groups: a main site
// owns a base domain and secondaries occupy subdomains of it.
func NewGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Group the current site under a main site as a subdomain",
		Long: "Group sites so they share one base domain. Run from a secondary " +
			"site's directory: `lerd group add <main> <label>` makes the current " +
			"site available at <label>.<main-domain>.",
	}
	cmd.AddCommand(newGroupAddCmd())
	cmd.AddCommand(newGroupRemoveCmd())
	cmd.AddCommand(newGroupLabelCmd())
	cmd.AddCommand(newGroupDBCmd())
	cmd.AddCommand(newGroupListCmd())
	return cmd
}

var groupShareDB bool

func newGroupAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <main-site> <label>",
		Short: "Group the current site under <main-site> at the <label> subdomain",
		Args:  cobra.ExactArgs(2),
		RunE:  runGroupAdd,
	}
	cmd.Flags().BoolVar(&groupShareDB, "share-db", false, "share the main site's database instead of keeping a separate one")
	return cmd
}

func newGroupDBCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "db <share|separate>",
		Short: "Switch the current secondary between sharing the main's database and its own",
		Args:  cobra.ExactArgs(1),
		RunE:  runGroupDB,
	}
}

func newGroupRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Ungroup the current secondary site, restoring a standalone domain",
		Args:  cobra.NoArgs,
		RunE:  runGroupRemove,
	}
}

func newGroupLabelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "label <new-label>",
		Short: "Change the subdomain label of the current secondary site",
		Args:  cobra.ExactArgs(1),
		RunE:  runGroupLabel,
	}
}

func newGroupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all site groups and their members",
		Args:  cobra.NoArgs,
		RunE:  runGroupList,
	}
}

func runGroupAdd(_ *cobra.Command, args []string) error {
	secondary, err := resolveSiteForCwd()
	if err != nil {
		return err
	}
	main, err := config.FindSite(args[0])
	if err != nil {
		if main, err = config.FindSiteByDomain(args[0]); err != nil {
			return fmt.Errorf("main site %q not found", args[0])
		}
	}
	label := strings.ToLower(strings.TrimSpace(args[1]))
	if err := grouping.AssignSecondary(main, secondary, label, groupShareDB); err != nil {
		return err
	}
	fmt.Printf("Grouped %s under %s at %s\n", secondary.Name, main.Name, secondary.PrimaryDomain())
	if groupShareDB {
		fmt.Printf("Sharing the %s database\n", main.Name)
	}
	return nil
}

func runGroupDB(_ *cobra.Command, args []string) error {
	secondary, err := resolveSiteForCwd()
	if err != nil {
		return err
	}
	var share bool
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "share", "shared", "on":
		share = true
	case "separate", "own", "off":
		share = false
	default:
		return fmt.Errorf("expected 'share' or 'separate', got %q", args[0])
	}
	if err := grouping.SetSecondarySharedDB(secondary, share); err != nil {
		return err
	}
	if share {
		fmt.Println("Now sharing the main site's database")
	} else {
		fmt.Println("Now using a separate database")
	}
	return nil
}

func runGroupRemove(_ *cobra.Command, _ []string) error {
	secondary, err := resolveSiteForCwd()
	if err != nil {
		return err
	}
	if err := grouping.UnassignSecondary(secondary); err != nil {
		return err
	}
	fmt.Printf("Ungrouped %s -> %s\n", secondary.Name, secondary.PrimaryDomain())
	return nil
}

func runGroupLabel(_ *cobra.Command, args []string) error {
	secondary, err := resolveSiteForCwd()
	if err != nil {
		return err
	}
	label := strings.ToLower(strings.TrimSpace(args[0]))
	if err := grouping.SetSecondaryLabel(secondary, label); err != nil {
		return err
	}
	fmt.Printf("Changed subdomain to %s\n", secondary.PrimaryDomain())
	return nil
}

func runGroupList(_ *cobra.Command, _ []string) error {
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	groups := map[string][]config.Site{}
	mains := map[string]config.Site{}
	for _, s := range reg.Sites {
		if s.Group == "" {
			continue
		}
		if s.IsGroupMain() {
			mains[s.Group] = s
		} else {
			groups[s.Group] = append(groups[s.Group], s)
		}
	}
	if len(mains) == 0 {
		fmt.Println("No site groups.")
		return nil
	}
	for group, main := range mains {
		fmt.Printf("%s (%s)\n", group, main.PrimaryDomain())
		for _, sec := range groups[group] {
			fmt.Printf("  %s -> %s\n", sec.Name, sec.PrimaryDomain())
		}
	}
	return nil
}
