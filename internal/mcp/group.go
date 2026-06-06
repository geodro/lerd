package mcp

import (
	"fmt"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/grouping"
)

// secondaryForPath resolves the site registered at the MCP-provided path, which
// for grouping is always the secondary the action operates on.
func secondaryForPath(args map[string]any) (*config.Site, *rpcError, string) {
	projectPath := resolvedPath(args)
	if projectPath == "" {
		return nil, nil, "path is required — pass a path argument or open Claude in the project directory"
	}
	site, err := config.FindSiteByPath(projectPath)
	if err != nil {
		return nil, nil, fmt.Sprintf("no site registered for %s — run site_link first", projectPath)
	}
	return site, nil, ""
}

func execSiteGroupAssign(args map[string]any) (any, *rpcError) {
	secondary, rpc, msg := secondaryForPath(args)
	if rpc != nil {
		return nil, rpc
	}
	if msg != "" {
		return toolErr(msg), nil
	}
	mainRef := strArg(args, "main")
	label := strings.ToLower(strings.TrimSpace(strArg(args, "label")))
	if mainRef == "" || label == "" {
		return toolErr("main and label are required for assign"), nil
	}
	main, err := config.FindSite(mainRef)
	if err != nil {
		if main, err = config.FindSiteByDomain(mainRef); err != nil {
			return toolErr(fmt.Sprintf("main site %q not found", mainRef)), nil
		}
	}
	if err := grouping.AssignSecondary(main, secondary, label, boolArg(args, "share_db")); err != nil {
		return toolErr(err.Error()), nil
	}
	out := fmt.Sprintf("Grouped %s under %s at %s", secondary.Name, main.Name, secondary.PrimaryDomain())
	if boolArg(args, "share_db") {
		out += fmt.Sprintf("\nSharing the %s database", main.Name)
	}
	return toolOK(out), nil
}

func execSiteGroupUnassign(args map[string]any) (any, *rpcError) {
	secondary, rpc, msg := secondaryForPath(args)
	if rpc != nil {
		return nil, rpc
	}
	if msg != "" {
		return toolErr(msg), nil
	}
	if err := grouping.UnassignSecondary(secondary); err != nil {
		return toolErr(err.Error()), nil
	}
	return toolOK(fmt.Sprintf("Ungrouped %s -> %s", secondary.Name, secondary.PrimaryDomain())), nil
}

func execSiteGroupLabel(args map[string]any) (any, *rpcError) {
	secondary, rpc, msg := secondaryForPath(args)
	if rpc != nil {
		return nil, rpc
	}
	if msg != "" {
		return toolErr(msg), nil
	}
	label := strings.ToLower(strings.TrimSpace(strArg(args, "label")))
	if label == "" {
		return toolErr("label is required"), nil
	}
	if err := grouping.SetSecondaryLabel(secondary, label); err != nil {
		return toolErr(err.Error()), nil
	}
	return toolOK("Changed subdomain to " + secondary.PrimaryDomain()), nil
}

func execSiteGroupDB(args map[string]any) (any, *rpcError) {
	secondary, rpc, msg := secondaryForPath(args)
	if rpc != nil {
		return nil, rpc
	}
	if msg != "" {
		return toolErr(msg), nil
	}
	var share bool
	switch strings.ToLower(strings.TrimSpace(strArg(args, "db"))) {
	case "share", "shared", "on", "true":
		share = true
	case "separate", "own", "off", "false":
		share = false
	default:
		return toolErr("db must be 'share' or 'separate'"), nil
	}
	if err := grouping.SetSecondarySharedDB(secondary, share); err != nil {
		return toolErr(err.Error()), nil
	}
	if share {
		return toolOK("Now sharing the main site's database"), nil
	}
	return toolOK("Now using a separate database"), nil
}

func execSiteGroupList(_ map[string]any) (any, *rpcError) {
	reg, err := config.LoadSites()
	if err != nil {
		return toolErr(err.Error()), nil
	}
	mains := map[string]config.Site{}
	secs := map[string][]config.Site{}
	for _, s := range reg.Sites {
		if s.Group == "" {
			continue
		}
		if s.IsGroupMain() {
			mains[s.Group] = s
		} else {
			secs[s.Group] = append(secs[s.Group], s)
		}
	}
	if len(mains) == 0 {
		return toolOK("No site groups."), nil
	}
	var b strings.Builder
	for group, main := range mains {
		fmt.Fprintf(&b, "%s (%s)\n", group, main.PrimaryDomain())
		for _, sec := range secs[group] {
			db := ""
			if sec.GroupSharedDB {
				db = " [shared db]"
			}
			fmt.Fprintf(&b, "  %s -> %s%s\n", sec.Name, sec.PrimaryDomain(), db)
		}
	}
	return toolOK(strings.TrimRight(b.String(), "\n")), nil
}
