package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/spf13/cobra"
)

// NewWorktreeCmd returns the `lerd worktree` parent command, mirroring
// `git worktree`'s subcommand layout. Today only `add` is implemented; we
// can grow `list` / `remove` later if there's demand.
func NewWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage git worktrees with lerd's setup pipeline",
	}
	cmd.AddCommand(newWorktreeAddCmd())
	cmd.AddCommand(newWorktreeRemoveCmd())
	return cmd
}

// newWorktreeAddCmd is the `lerd worktree add` subcommand. All arguments are
// forwarded verbatim to `git worktree add`, so every git flag works (-b,
// --detach, --track, --lock, etc.). After git completes, the wrapper waits
// for lerd's watcher-driven install pipeline, presents a unified asset-worker
// / npm-build prompt (eligible per_worktree+replaces_build workers + npm
// production-build scripts + Skip), and prompts for DB isolation. LAN share
// is intentionally not prompted.
func newWorktreeAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "add [git-worktree-add args...]",
		Short:              "Create a git worktree (any git flags) and run lerd's interactive setup (asset-worker / build prompt + DB isolation)",
		DisableFlagParsing: true,
		SilenceUsage:       true,
		Args:               cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if hasHelpFlag(args) {
				return c.Help()
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			site, err := config.FindSiteByPath(cwd)
			if err != nil {
				return fmt.Errorf("not inside a registered lerd site (cwd=%s)", cwd)
			}

			gitArgs := append([]string{"worktree", "add"}, args...)
			fmt.Printf("Running: git %s\n", strings.Join(gitArgs, " "))
			gitCmd := exec.Command("git", gitArgs...)
			gitCmd.Stdout = os.Stdout
			gitCmd.Stderr = os.Stderr
			if err := gitCmd.Run(); err != nil {
				return fmt.Errorf("git worktree add: %w", err)
			}

			worktreePath, branch, err := newestWorktree(cwd)
			if err != nil {
				fmt.Printf("[WARN] could not locate the new worktree on disk: %v\n", err)
				return nil
			}

			fmt.Println("Waiting for lerd to install dependencies (composer + JS)...")
			if err := waitForWorktreeReady(worktreePath, 5*time.Minute); err != nil {
				fmt.Printf("[WARN] %v — you can rerun setup later by editing the worktree.\n", err)
			} else {
				fmt.Println("Dependencies installed.")
			}

			if optedIn := OptedInHostWorkers(site, worktreePath); len(optedIn) > 0 {
				fmt.Printf("Auto-starting opted-in workers: %s\n", strings.Join(optedIn, ", "))
			}
			switch choice := promptWorktreeBuild(site, worktreePath); choice.kind {
			case "worker":
				if err := WorkerStartForSite(site.Name, worktreePath, site.PHPVersion, choice.value, choice.worker, false); err != nil {
					fmt.Printf("[WARN] failed to start %s: %v — assets will not be served, run `lerd worker start %s` or `npm run build` manually.\n", choice.value, err, choice.value)
				} else {
					fmt.Printf("Started %s, skipping build — it will provide assets.\n", choice.value)
				}
			case "script":
				fmt.Printf("Running npm run %s...\n", choice.value)
				if err := gitpkg.RunNpmScript(worktreePath, choice.value); err != nil {
					fmt.Printf("[WARN] npm run %s failed: %v, first request will throw ViteManifestNotFoundException; rerun manually after fixing.\n", choice.value, err)
				} else {
					fmt.Println("Frontend built.")
				}
			}

			if err := promptDBIsolation(site, branch); err != nil {
				fmt.Printf("[WARN] DB setup skipped: %v\n", err)
			}

			scheme := "http"
			if site.Secured {
				scheme = "https"
			}
			fmt.Printf("\nWorktree ready: %s://%s.%s\n", scheme, branch, site.PrimaryDomain())
			return nil
		},
	}
	return cmd
}

// worktreeBuildChoice tags the user's pick from promptWorktreeBuild so the
// caller can branch without parsing strings. kind is "worker" (asset worker
// will serve assets, skip build), "script" (run `npm run <value>`), or
// "skip" (do nothing). When kind == "worker" the worker field carries the
// FrameworkWorker so the caller can start it without re-resolving.
type worktreeBuildChoice struct {
	kind   string
	value  string
	worker config.FrameworkWorker
}

// promptWorktreeBuild merges the asset-worker decision and the npm-build
// decision into one select. Options include every framework worker eligible
// to replace the build for this worktree (per_worktree + replaces_build +
// check passes) — even ones the user hasn't opted into via .lerd.yaml — plus
// each available package.json build script. The default is the first
// opted-in asset worker, then the first build script, then skip. Workers
// that aren't opted in still appear so the user can start them ad-hoc for
// this worktree without editing parent yaml.
func promptWorktreeBuild(site *config.Site, worktreePath string) worktreeBuildChoice {
	eligible := eligibleBuildReplacers(site, worktreePath)
	scripts := availableBuildScripts(worktreePath)
	if len(eligible) == 0 && len(scripts) == 0 {
		return worktreeBuildChoice{kind: "skip"}
	}

	var workers map[string]config.FrameworkWorker
	if fw, ok := config.GetFrameworkForDir(site.Framework, site.Path); ok {
		workers = fw.Workers
	}
	optedSet := make(map[string]bool)
	for _, n := range OptedInBuildReplacers(site, worktreePath) {
		optedSet[n] = true
	}

	options := make([]huh.Option[string], 0, len(eligible)+len(scripts)+1)
	var defaultVal string
	for _, name := range eligible {
		label := name
		if w, ok := workers[name]; ok && w.Label != "" {
			label = w.Label
		}
		val := "worker:" + name
		options = append(options, huh.NewOption(fmt.Sprintf("Use %s (asset worker)", label), val))
		if optedSet[name] && defaultVal == "" {
			defaultVal = val
		}
	}
	for _, s := range scripts {
		val := "script:" + s
		options = append(options, huh.NewOption("npm run "+s, val))
		if defaultVal == "" {
			defaultVal = val
		}
	}
	options = append(options, huh.NewOption("Skip — I'll run npm run dev (or build) myself", "skip"))

	picked := defaultVal
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Build the worktree's frontend assets?").
			Description("Skipping means the first request will throw ViteManifestNotFoundException until you run a build or `npm run dev`.").
			Options(options...).
			Value(&picked),
	))
	if err := form.Run(); err != nil {
		return worktreeBuildChoice{kind: "skip"}
	}
	switch {
	case picked == "skip":
		return worktreeBuildChoice{kind: "skip"}
	case strings.HasPrefix(picked, "worker:"):
		name := strings.TrimPrefix(picked, "worker:")
		return worktreeBuildChoice{kind: "worker", value: name, worker: workers[name]}
	default:
		return worktreeBuildChoice{kind: "script", value: strings.TrimPrefix(picked, "script:")}
	}
}

// availableBuildScripts returns the production-build-style scripts declared
// in package.json, in preference order. `dev` is intentionally excluded —
// it's a long-running watcher, not a one-shot the wrapper should spawn.
func availableBuildScripts(worktreePath string) []string {
	pkgScripts := readPackageScripts(worktreePath)
	if pkgScripts == nil {
		return nil
	}
	candidates := []string{"build", "prod", "build:prod", "build-prod", "production"}
	var out []string
	for _, c := range candidates {
		if _, ok := pkgScripts[c]; ok {
			out = append(out, c)
		}
	}
	return out
}

func readPackageScripts(worktreePath string) map[string]string {
	data, err := os.ReadFile(filepath.Join(worktreePath, "package.json"))
	if err != nil {
		return nil
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	return pkg.Scripts
}

// hasHelpFlag returns true if any arg is `-h` or `--help`. Used by the
// passthrough commands which disable cobra's own flag parsing.
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// newestWorktree finds the most recently created worktree under sitePath and
// returns its checkout path and sanitized branch. Used after `git worktree
// add` returns so the wrapper doesn't need to know which positional arg was
// the path or the branch.
func newestWorktree(sitePath string) (string, string, error) {
	worktreesDir := filepath.Join(sitePath, ".git", "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return "", "", err
	}
	var newest os.DirEntry
	var newestMtime time.Time
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == nil || info.ModTime().After(newestMtime) {
			newest = e
			newestMtime = info.ModTime()
		}
	}
	if newest == nil {
		return "", "", fmt.Errorf("no worktrees found")
	}
	wtMeta := filepath.Join(worktreesDir, newest.Name())
	gitdirData, err := os.ReadFile(filepath.Join(wtMeta, "gitdir"))
	if err != nil {
		return "", "", err
	}
	checkout := filepath.Dir(filepath.Clean(strings.TrimSpace(string(gitdirData))))
	headData, _ := os.ReadFile(filepath.Join(wtMeta, "HEAD"))
	line := strings.TrimSpace(string(headData))
	branch := "detached"
	if strings.HasPrefix(line, "ref: refs/heads/") {
		branch = gitpkg.SanitizeBranch(strings.TrimPrefix(line, "ref: refs/heads/"))
	} else if len(line) >= 7 {
		branch = "detached-" + line[:7]
	}
	return checkout, branch, nil
}

// waitForWorktreeReady polls until the worktree's vendor + node_modules +
// .env are in place, signalling that lerd's watcher-driven install pipeline
// has finished. The frontend build is no longer part of this wait — `lerd
// worktree add` invokes RunFrontendBuild explicitly after installs succeed.
func waitForWorktreeReady(worktreePath string, deadline time.Duration) error {
	end := time.Now().Add(deadline)
	hasComposer := fileExistsAt(filepath.Join(worktreePath, "composer.json"))
	hasJS := fileExistsAt(filepath.Join(worktreePath, "package.json"))
	for time.Now().Before(end) {
		envOk := fileExistsAt(filepath.Join(worktreePath, ".env"))
		composerOk := !hasComposer || fileExistsAt(filepath.Join(worktreePath, "vendor", "autoload.php"))
		jsOk := !hasJS || fileExistsAt(filepath.Join(worktreePath, "node_modules"))
		if envOk && composerOk && jsOk {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for worktree setup", deadline)
}

func fileExistsAt(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// promptDBIsolation asks the user how the worktree's database should be
// configured. Default: share the parent's database (no isolation). The other
// options call into the same helpers the dashboard toggle uses.
func promptDBIsolation(site *config.Site, branch string) error {
	type choice string
	const (
		share     choice = "share"
		empty     choice = "empty"
		cloneMain choice = "clone-main"
	)

	preserved, hasPreserved, _ := config.FindWorktreeDB(site.Name, branch)
	const (
		reuse choice = "reuse"
		reset choice = "reset"
	)

	var options []huh.Option[choice]
	if hasPreserved {
		options = append(options,
			huh.NewOption(
				fmt.Sprintf("Reuse preserved isolated DB %q", preserved.DBName),
				reuse,
			),
			huh.NewOption(
				fmt.Sprintf("Reset preserved DB %q to a fresh empty schema (drops existing data)", preserved.DBName),
				reset,
			),
		)
	}
	options = append(options,
		huh.NewOption("Share parent's database", share),
	)
	if !hasPreserved {
		options = append(options, huh.NewOption("Isolated DB, empty schema", empty))
	}
	options = append(options,
		huh.NewOption("Isolated DB, cloned from main (mysqldump | mysql or pg_dump | psql)", cloneMain),
	)
	for _, e := range branchesWithIsolatedDB(site) {
		if e == branch {
			continue
		}
		options = append(options, huh.NewOption("Isolated DB, cloned from "+e, choice("clone-"+e)))
	}

	var picked choice
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[choice]().
			Title("Database for this worktree").
			Options(options...).
			Value(&picked),
	))
	if err := form.Run(); err != nil {
		return err
	}

	switch {
	case picked == share:
		return nil
	case picked == reuse:
		// CreateDatabase inside SetWorktreeDBIsolated is a no-op when the
		// schema already exists, so calling with source="empty" simply
		// reconnects the worktree to its preserved data without touching
		// any tables.
		return SetWorktreeDBIsolated(site, branch, true, "empty")
	case picked == reset:
		// Drop the preserved DB so SetWorktreeDBIsolated's CREATE produces
		// a truly empty schema, then offer migrations like the standard
		// empty path.
		if hasPreserved {
			if _, err := DropDatabase(preserved.Service, preserved.DBName); err != nil {
				return fmt.Errorf("dropping preserved DB %q: %w", preserved.DBName, err)
			}
			_, _, _ = config.RemoveWorktreeDB(site.Name, branch)
		}
		if err := SetWorktreeDBIsolated(site, branch, true, "empty"); err != nil {
			return err
		}
		return promptRunMigrations(site, branch)
	case picked == empty:
		if err := SetWorktreeDBIsolated(site, branch, true, "empty"); err != nil {
			return err
		}
		// An empty schema is rarely useful on its own — Laravel apps need
		// at least the migrations table populated. Offer to run them now.
		return promptRunMigrations(site, branch)
	case picked == cloneMain:
		return SetWorktreeDBIsolated(site, branch, true, "main")
	default:
		// "clone-<branch>"
		src := strings.TrimPrefix(string(picked), "clone-")
		return SetWorktreeDBIsolated(site, branch, true, src)
	}
}

// promptRunMigrations asks whether to run the framework's migration command
// against the freshly-created empty database. Yes runs `php artisan migrate
// --force` inside the worktree's checkout via the FPM container.
func promptRunMigrations(site *config.Site, branch string) error {
	wtPath, err := worktreePathForBranch(site, branch)
	if err != nil || !fileExistsAt(filepath.Join(wtPath, "artisan")) {
		// Not a Laravel project (or the worktree disappeared); silently
		// skip the prompt — non-Laravel apps have their own migration
		// tooling that we don't try to second-guess.
		return nil
	}

	var picked string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Run database migrations on the new isolated database?").
			Options(
				huh.NewOption("Skip — I'll run migrations myself", ""),
				huh.NewOption("Run `php artisan migrate --force` now", "migrate"),
			).
			Value(&picked),
	))
	if err := form.Run(); err != nil {
		return err
	}
	if picked != "migrate" {
		return nil
	}

	cmd := exec.Command(filepath.Join(config.BinDir(), "php"), "artisan", "migrate", "--force")
	cmd.Dir = wtPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("artisan migrate: %w", err)
	}
	return nil
}

// worktreePathForBranch resolves the on-disk checkout path for a branch by
// walking the parent's worktree metadata. Returns "" / error when the branch
// has no worktree (e.g., the user removed it before the prompt finished).
func worktreePathForBranch(site *config.Site, branch string) (string, error) {
	worktrees, err := gitpkg.DetectWorktrees(site.Path, site.PrimaryDomain())
	if err != nil {
		return "", err
	}
	for _, wt := range worktrees {
		if wt.Branch == branch {
			return wt.Path, nil
		}
	}
	return "", fmt.Errorf("worktree %q not found", branch)
}

// eligibleBuildReplacers returns every framework worker eligible to provide
// assets at the given path: replaces_build:true, per_worktree:true (when
// path is a worktree), and Check rule matches. Unlike OptedInBuildReplacers
// it does NOT require the worker to be in the parent's .lerd.yaml workers:
// list, so the worktree-add prompt can offer asset workers the user hasn't
// explicitly opted into yet.
func eligibleBuildReplacers(site *config.Site, path string) []string {
	if site.Framework == "" {
		return nil
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok {
		return nil
	}
	isWorktree := path != site.Path
	var out []string
	for name, w := range fw.Workers {
		if !w.ReplacesBuild {
			continue
		}
		if isWorktree && !w.IsPerWorktree() {
			continue
		}
		if w.Check != nil && !config.MatchesRule(path, *w.Check) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// OptedInBuildReplacers returns names of workers (a) opted into via
// .lerd.yaml workers:, (b) declared replaces_build:true in the framework
// yaml, and (c) able to run at the given path. When path != site.Path the
// per_worktree:true gate applies; for the parent it doesn't.
func OptedInBuildReplacers(site *config.Site, path string) []string {
	if site.Framework == "" {
		return nil
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok {
		return nil
	}
	proj, _ := config.LoadProjectConfig(site.Path)
	if proj == nil || len(proj.Workers) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(proj.Workers))
	for _, n := range proj.Workers {
		wanted[n] = true
	}
	isWorktree := path != site.Path
	var out []string
	for name, w := range fw.Workers {
		if !wanted[name] || !w.ReplacesBuild {
			continue
		}
		if isWorktree && !w.IsPerWorktree() {
			continue
		}
		if w.Check != nil && !config.MatchesRule(path, *w.Check) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// AutoStartOptedInWorktreeWorkers writes (and starts, on Linux) every
// host worker the user opted into via the parent's .lerd.yaml workers
// list, scoped to the given worktree path. Idempotent — used by the
// watcher's onAdded hook AND by the daemon's boot-time scanWorktrees pass
// so per-worktree units survive a daemon restart cleanly. Errors are
// surfaced as warnings so a single broken unit doesn't block siblings.
func AutoStartOptedInWorktreeWorkers(site *config.Site, worktreePath, phpVersion string) {
	if site == nil || worktreePath == "" {
		return
	}
	for _, name := range OptedInHostWorkers(site, worktreePath) {
		fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
		if !ok {
			return
		}
		w, ok := fw.Workers[name]
		if !ok {
			continue
		}
		if err := WorkerStartForSite(site.Name, worktreePath, phpVersion, name, w, false); err != nil {
			fmt.Printf("[WARN] auto-start %s for worktree %s: %v\n", name, filepath.Base(worktreePath), err)
		}
	}
}

// OptedInHostWorkers returns the names of host-mode workers the user has
// opted into for this project (.lerd.yaml workers:) and whose check rule
// matches the worktree path. Worker auto-start now follows project intent
// instead of treating every host:true worker as implicitly desired.
func OptedInHostWorkers(site *config.Site, worktreePath string) []string {
	if site.Framework == "" {
		return nil
	}
	fw, ok := config.GetFrameworkForDir(site.Framework, site.Path)
	if !ok {
		return nil
	}
	proj, _ := config.LoadProjectConfig(site.Path)
	if proj == nil || len(proj.Workers) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(proj.Workers))
	for _, n := range proj.Workers {
		wanted[n] = true
	}
	var out []string
	for name, w := range fw.Workers {
		if !w.Host || !wanted[name] || !w.IsPerWorktree() {
			continue
		}
		if w.Check != nil && !config.MatchesRule(worktreePath, *w.Check) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func branchesWithIsolatedDB(site *config.Site) []string {
	entries, err := config.WorktreeDBsForSite(site.Name)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Branch)
	}
	return out
}
