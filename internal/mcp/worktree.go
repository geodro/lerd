package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
)

// worktreeTool returns the MCP tool descriptor for the worktree dispatcher.
// Actions shell out to `git worktree …` and `lerd db:isolate` / `lerd
// db:share` so the cli stays the single source of truth for the lifecycle;
// directly importing cli here would create an import cycle (cli/mcp.go
// imports mcp for Serve()).
func worktreeTool() mcpTool {
	return mcpTool{
		Name:        "worktree",
		Description: "Manage git worktrees. list / add / remove / db_isolate (empty|main|<branch>) / db_share. Watcher auto-installs deps on add and presents a unified asset-worker / npm-build prompt (workers with replaces_build+per_worktree appear alongside npm scripts; picked workers start ad-hoc with persist=false). Worktrees on secured sites get *.branch.domain.test wildcard cert SANs and nginx server_name automatically. .lerd.yaml env_overrides ({{domain}}/{{scheme}}/{{site}} placeholders) layers per-worktree env vars on top of the APP_URL rewrite. Workers with per_worktree:true run under lerd-<wname>-<site>-<wt> units. Remove keeps isolated DB unless keep_db=false.",
		InputSchema: mcpSchema{
			Type: "object",
			Properties: map[string]mcpProp{
				"action":   {Type: "string", Enum: []string{"list", "add", "remove", "db_isolate", "db_share"}},
				"site":     {Type: "string", Description: "Defaults to cwd's site."},
				"branch":   {Type: "string"},
				"git_args": {Type: "array", Description: "Forwarded to git worktree."},
				"force":    {Type: "boolean", Description: "remove: --force."},
				"keep_db":  {Type: "boolean", Description: "remove: preserve DB (default true)."},
				"source":   {Type: "string", Description: "db_isolate seed."},
			},
			Required: []string{"action"},
		},
	}
}

func dispatchWorktree(args map[string]any) (any, *rpcError) {
	switch strArg(args, "action") {
	case "list":
		return execWorktreeList(args)
	case "add":
		return execWorktreeAdd(args)
	case "remove":
		return execWorktreeRemove(args)
	case "db_isolate":
		return execWorktreeDBIsolate(args)
	case "db_share":
		return execWorktreeDBShare(args)
	default:
		return toolErr("unknown action for tool \"worktree\""), nil
	}
}

// resolveSiteForWorktree returns the resolved site or an MCP-shaped error
// payload (the second return). When err is non-nil, callers should return
// (err, nil) verbatim — toolErr produces the JSON the MCP host expects.
func resolveSiteForWorktree(args map[string]any) (*config.Site, map[string]any) {
	if name := strArg(args, "site"); name != "" {
		s, err := config.FindSite(name)
		if err != nil {
			return nil, toolErr(fmt.Sprintf("unknown site %q: %v", name, err))
		}
		return s, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, toolErr("getwd: " + err.Error())
	}
	if s, err := config.FindSiteByPath(cwd); err == nil {
		return s, nil
	}
	if s, ok := config.ParentSiteForWorktreeDir(cwd); ok {
		return s, nil
	}
	return nil, toolErr("not inside a registered site or worktree (cwd=" + cwd + "); pass site=<name>")
}

type worktreeOut struct {
	Branch      string `json:"branch"`
	Domain      string `json:"domain,omitempty"`
	Path        string `json:"path,omitempty"`
	DBIsolated  bool   `json:"db_isolated,omitempty"`
	DBName      string `json:"db_name,omitempty"`
	DBService   string `json:"db_service,omitempty"`
	LANPort     int    `json:"lan_port,omitempty"`
	PreservedDB bool   `json:"preserved_db,omitempty"`
}

func execWorktreeList(args map[string]any) (any, *rpcError) {
	site, errResp := resolveSiteForWorktree(args)
	if errResp != nil {
		return errResp, nil
	}
	worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil {
		return toolErr("detect worktrees: " + err.Error()), nil
	}
	out := make([]worktreeOut, 0, len(worktrees))
	live := map[string]bool{}
	for _, wt := range worktrees {
		live[wt.Branch] = true
		entry := worktreeOut{Branch: wt.Branch, Domain: wt.Domain, Path: wt.Path}
		if e, ok, _ := config.FindWorktreeDB(site.Name, wt.Branch); ok {
			entry.DBIsolated = true
			entry.DBName = e.DBName
			entry.DBService = e.Service
		}
		if e, ok, _ := config.FindWorktreeLAN(site.Name, wt.Branch); ok {
			entry.LANPort = e.Port
		}
		out = append(out, entry)
	}
	if dbs, err := config.WorktreeDBsForSite(site.Name); err == nil {
		for _, e := range dbs {
			if live[e.Branch] {
				continue
			}
			out = append(out, worktreeOut{
				Branch:      e.Branch,
				DBIsolated:  true,
				DBName:      e.DBName,
				DBService:   e.Service,
				PreservedDB: true,
			})
		}
	}
	return toolJSON(map[string]any{"site": site.Name, "worktrees": out}), nil
}

func execWorktreeAdd(args map[string]any) (any, *rpcError) {
	site, errResp := resolveSiteForWorktree(args)
	if errResp != nil {
		return errResp, nil
	}
	gitArgs := []string{"worktree", "add"}
	extra, _ := args["git_args"].([]any)
	if len(extra) == 0 {
		branch := strArg(args, "branch")
		if branch == "" {
			return toolErr("branch or git_args required"), nil
		}
		gitArgs = append(gitArgs, branch)
	} else {
		for _, v := range extra {
			gitArgs = append(gitArgs, fmt.Sprint(v))
		}
	}
	out, err := runIn(site.Path, "git", gitArgs...)
	if err != nil {
		return toolErr("git " + strings.Join(gitArgs, " ") + ": " + out), nil
	}
	return toolJSON(map[string]any{"ok": true, "site": site.Name, "output": out}), nil
}

func execWorktreeRemove(args map[string]any) (any, *rpcError) {
	site, errResp := resolveSiteForWorktree(args)
	if errResp != nil {
		return errResp, nil
	}
	branch := strArg(args, "branch")
	if branch == "" {
		return toolErr("branch required"), nil
	}
	keepDB := true
	if v, ok := args["keep_db"].(bool); ok {
		keepDB = v
	}
	sanitized := gitpkg.SanitizeBranch(branch)

	// If the caller wants the DB dropped, do it FIRST while the worktree
	// path is still resolvable — `lerd db:share` needs the worktree dir on
	// disk to find the branch in the registry. After git removes the
	// worktree the path is gone, so a deferred drop wouldn't work.
	dbDropped := false
	if !keepDB {
		if wtPath := worktreePathFor(site, sanitized); wtPath != "" {
			if _, err := runIn(wtPath, "lerd", "db:share"); err == nil {
				dbDropped = true
			}
		}
	}

	gitArgs := []string{"worktree", "remove"}
	if boolArg(args, "force") {
		gitArgs = append(gitArgs, "--force")
	}
	gitArgs = append(gitArgs, branch)
	out, err := runIn(site.Path, "git", gitArgs...)
	if err != nil {
		return toolErr("git " + strings.Join(gitArgs, " ") + ": " + out), nil
	}
	return toolJSON(map[string]any{
		"ok":                  true,
		"site":                site.Name,
		"output":              out,
		"isolated_db_dropped": dbDropped,
	}), nil
}

func execWorktreeDBIsolate(args map[string]any) (any, *rpcError) {
	site, errResp := resolveSiteForWorktree(args)
	if errResp != nil {
		return errResp, nil
	}
	branch := branchFromArgs(args, site)
	if branch == "" {
		return toolErr("branch required (or run from inside the worktree's checkout)"), nil
	}
	source := strArg(args, "source")
	if source == "" {
		source = "empty"
	}
	wtPath := worktreePathFor(site, branch)
	if wtPath == "" {
		return toolErr("worktree path for branch " + branch + " not on disk"), nil
	}
	cliArgs := []string{"db:isolate", "--source", source}
	out, err := runIn(wtPath, "lerd", cliArgs...)
	if err != nil {
		return toolErr("lerd " + strings.Join(cliArgs, " ") + ": " + out), nil
	}
	resp := map[string]any{"ok": true, "site": site.Name, "branch": branch, "source": source, "output": out}
	if e, ok, _ := config.FindWorktreeDB(site.Name, branch); ok {
		resp["db_name"] = e.DBName
		resp["db_service"] = e.Service
	}
	return toolJSON(resp), nil
}

func execWorktreeDBShare(args map[string]any) (any, *rpcError) {
	site, errResp := resolveSiteForWorktree(args)
	if errResp != nil {
		return errResp, nil
	}
	branch := branchFromArgs(args, site)
	if branch == "" {
		return toolErr("branch required (or run from inside the worktree's checkout)"), nil
	}
	wtPath := worktreePathFor(site, branch)
	if wtPath == "" {
		return toolErr("worktree path for branch " + branch + " not on disk"), nil
	}
	out, err := runIn(wtPath, "lerd", "db:share")
	if err != nil {
		return toolErr("lerd db:share: " + out), nil
	}
	return toolJSON(map[string]any{"ok": true, "site": site.Name, "branch": branch, "output": out}), nil
}

func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func worktreePathFor(site *config.Site, sanitizedBranch string) string {
	wts, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil {
		return ""
	}
	for _, wt := range wts {
		if wt.Branch == sanitizedBranch {
			return wt.Path
		}
	}
	return ""
}

func branchFromArgs(args map[string]any, site *config.Site) string {
	if b := strArg(args, "branch"); b != "" {
		return gitpkg.SanitizeBranch(b)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	abs, _ := filepath.Abs(cwd)
	wts, _ := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	for _, wt := range wts {
		if wt.Path == abs {
			return wt.Branch
		}
	}
	return ""
}
