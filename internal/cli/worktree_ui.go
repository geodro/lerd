package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
)

// warningCapturingWriter forwards every write to the underlying writer while
// scanning for lines prefixed with "[WARN]" so RunWorktreeAdd can return them
// to the caller. RunWorktreeAdd treats most failures (timeout waiting for
// installs, build script error, db setup skipped) as soft warnings and keeps
// going — the dashboard needs to know they happened so it doesn't auto-close
// the modal on a half-finished setup.
type warningCapturingWriter struct {
	w        io.Writer
	buf      []byte
	warnings []string
}

func (c *warningCapturingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.buf = append(c.buf, p[:n]...)
	for {
		i := bytes.IndexByte(c.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(c.buf[:i]), "\r")
		c.buf = c.buf[i+1:]
		if strings.HasPrefix(strings.TrimSpace(line), "[WARN]") {
			c.warnings = append(c.warnings, line)
		}
	}
	return n, err
}

// WorktreeAddRequest carries the choices the dashboard (or any non-interactive
// caller) makes for `lerd worktree add`, mirroring what the CLI's huh prompts
// gather. Exactly one of NewBranch / ExistingBranch must be set.
type WorktreeAddRequest struct {
	NewBranch      string // create this branch with `git worktree add -b`
	ExistingBranch string // check out an already-existing branch
	BaseRef        string // start point for a new branch (optional; defaults to HEAD)
	DBChoice       string // "share" | "empty" | "clone-main" | "clone-<branch>" | "reuse" | "reset"
	RunMigrations  bool   // run `php artisan migrate --force` when DBChoice yields an empty schema
	Build          string // "auto" | "skip" | "worker:<name>" | "script:<name>"
}

// buildWorktreeAddGitArgs turns a WorktreeAddRequest + checkout path into the
// `git worktree add` argument list. New-branch form is `worktree add -b
// <branch> <path> [<base>]`; existing-branch form is `worktree add <path>
// <branch>`.
func buildWorktreeAddGitArgs(req WorktreeAddRequest, checkoutPath string) ([]string, error) {
	nb := strings.TrimSpace(req.NewBranch)
	eb := strings.TrimSpace(req.ExistingBranch)
	switch {
	case nb == "" && eb == "":
		return nil, fmt.Errorf("a new or existing branch is required")
	case nb != "" && eb != "":
		return nil, fmt.Errorf("specify either a new branch or an existing branch, not both")
	case nb != "":
		args := []string{"worktree", "add", "-b", nb, checkoutPath}
		if base := strings.TrimSpace(req.BaseRef); base != "" {
			args = append(args, base)
		}
		return args, nil
	default:
		return []string{"worktree", "add", checkoutPath, eb}, nil
	}
}

// ensureNestedWorktreeExclude appends "/<base>-*/" to .git/info/exclude in
// sitePath (idempotent) so nested worktree dirs don't show up in git status
// of the parent repo. No-op when .git isn't a dir or when basename
// contains gitignore meta-chars we'd have to escape.
func ensureNestedWorktreeExclude(sitePath string) error {
	gitInfo, err := os.Stat(filepath.Join(sitePath, ".git"))
	if err != nil || !gitInfo.IsDir() {
		return nil
	}
	base := filepath.Base(sitePath)
	if strings.ContainsAny(base, "[]?*!#\\") {
		return nil
	}
	pattern := "/" + base + "-*/"
	excludePath := filepath.Join(sitePath, ".git", "info", "exclude")
	existing, _ := os.ReadFile(excludePath)
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + pattern + "\n")
	return err
}

// WorktreeCheckoutPath returns the directory a new worktree for branch should
// be checked out into: a child of the parent site, "<sitePath>/<base>-<slug>"
// where <base> is filepath.Base(sitePath). Bumps a numeric suffix if the
// default path already exists. RunWorktreeAdd also writes a `/<base>-*/`
// pattern into .git/info/exclude so git status doesn't show siblings.
// Caveat: non-git tools (composer, IDEs, find/rsync/tar) walking the
// parent tree DO descend into the worktree — gitignore doesn't hide it
// from them. Most callers don't care; flag if you do.
func WorktreeCheckoutPath(sitePath, branch string) string {
	parentBase := filepath.Base(sitePath)
	base := filepath.Join(sitePath, parentBase+"-"+gitpkg.SanitizeBranch(branch))
	candidate := base
	for i := 2; ; i++ {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

// resolveBuildChoice maps a UI build request ("auto"|"skip"|"worker:<n>"|
// "script:<n>") to a concrete (kind, value): kind is "worker", "script" or
// "skip". eligible = workers able to replace the build at the target path;
// optedIn = the subset declared in the parent's .lerd.yaml; scripts =
// package.json build scripts. An unavailable worker/script falls back to the
// same default the CLI's "auto" path uses.
func resolveBuildChoice(requested string, eligible, optedIn, scripts []string) (kind, value string) {
	contains := func(ss []string, s string) bool {
		for _, x := range ss {
			if x == s {
				return true
			}
		}
		return false
	}
	auto := func() (string, string) {
		for _, n := range optedIn {
			if contains(eligible, n) {
				return "worker", n
			}
		}
		if len(scripts) > 0 {
			return "script", scripts[0]
		}
		return "skip", ""
	}
	switch {
	case requested == "skip":
		return "skip", ""
	case requested == "" || requested == "auto":
		return auto()
	case strings.HasPrefix(requested, "worker:"):
		if n := strings.TrimPrefix(requested, "worker:"); contains(eligible, n) {
			return "worker", n
		}
		return auto()
	case strings.HasPrefix(requested, "script:"):
		if n := strings.TrimPrefix(requested, "script:"); contains(scripts, n) {
			return "script", n
		}
		return auto()
	default:
		return auto()
	}
}

func logf(w io.Writer, format string, a ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format+"\n", a...)
}

// localBranchExists reports whether sitePath's repo has a local branch named b.
func localBranchExists(sitePath, b string) bool {
	return gitpkg.BranchExists(sitePath, b)
}

// normalizeAddRequest resolves the "existing branch" case: if the picked branch
// doesn't exist locally (it's a remote-tracking ref like "origin/feat-x", or
// any other start-point), it's turned into a new-branch request named after the
// ref's last path segment with the picked value as the start point, so `git
// worktree add -b feat-x <path> origin/feat-x` runs instead of failing.
func normalizeAddRequest(sitePath string, req WorktreeAddRequest) WorktreeAddRequest {
	eb := strings.TrimSpace(req.ExistingBranch)
	if eb == "" || localBranchExists(sitePath, eb) {
		return req
	}
	return WorktreeAddRequest{
		NewBranch:     eb[strings.LastIndex(eb, "/")+1:],
		BaseRef:       eb,
		DBChoice:      req.DBChoice,
		RunMigrations: req.RunMigrations,
		Build:         req.Build,
	}
}

// RunWorktreeAdd creates a git worktree for site, runs composer + JS
// install with output streamed to log, applies the build choice,
// configures the database, and optionally runs migrations. Progress lines
// are written to log (may be nil). Returns the sanitized branch name, the
// checkout path, and any "[WARN]" lines emitted along the way so callers
// (e.g. the dashboard) can keep the modal open and surface them instead
// of silently treating the setup as success.
//
// The install runs inside this process — not the watcher daemon — so its
// stdout/stderr lands directly in the dashboard's SSE stream. A cross-
// process flock (acquired before git worktree add so the watcher's
// fsnotify-triggered install loses the race) keeps the watcher from
// running its own composer install in parallel and clobbering vendor/.
func RunWorktreeAdd(site *config.Site, req WorktreeAddRequest, log io.Writer) (string, string, []string, error) {
	capturer := &warningCapturingWriter{w: log}
	log = capturer
	req = normalizeAddRequest(site.Path, req)
	branchInput := req.NewBranch
	if branchInput == "" {
		branchInput = req.ExistingBranch
	}
	branch := gitpkg.SanitizeBranch(branchInput)
	checkoutPath := WorktreeCheckoutPath(site.Path, branchInput)

	release, err := gitpkg.LockInstall(checkoutPath, 30*time.Second)
	if err != nil {
		return "", "", capturer.warnings, fmt.Errorf("acquire install lock: %w", err)
	}
	defer release()

	gitArgs, err := buildWorktreeAddGitArgs(req, checkoutPath)
	if err != nil {
		return "", "", capturer.warnings, err
	}
	logf(log, "Running: git %s", strings.Join(gitArgs, " "))
	if err := gitpkg.Run(site.Path, log, gitArgs...); err != nil {
		return "", "", capturer.warnings, fmt.Errorf("git worktree add: %w", err)
	}
	if err := ensureNestedWorktreeExclude(site.Path); err != nil {
		logf(log, "[WARN] writing .git/info/exclude: %v", err)
	}

	worktreeDomain := branch + "." + site.PrimaryDomain()
	logf(log, "Installing dependencies (composer + JS)...")
	warnsBefore := len(capturer.warnings)
	gitpkg.EnsureWorktreeDeps(site.Path, checkoutPath, worktreeDomain, site.Secured, log)
	if len(capturer.warnings) == warnsBefore {
		logf(log, "Dependencies installed.")
	}

	// Host-proxy worktrees have no asset build step; their dev server (wired by
	// the watcher's worktree sync) is the running process.
	if !site.IsHostProxy() {
		applyWorktreeBuildRequest(site, checkoutPath, req.Build, log)
	}

	if err := ApplyWorktreeDBChoice(site, branch, req.DBChoice, log); err != nil {
		logf(log, "[WARN] database setup skipped: %v", err)
	} else if req.RunMigrations && dbChoiceYieldsEmptySchema(req.DBChoice) {
		if err := RunWorktreeMigrations(site, branch, log); err != nil {
			logf(log, "[WARN] %v", err)
		}
	}

	scheme := "http"
	if site.Secured {
		scheme = "https"
	}
	logf(log, "Worktree ready: %s://%s.%s", scheme, branch, site.PrimaryDomain())
	return branch, checkoutPath, capturer.warnings, nil
}

// applyWorktreeBuildRequest resolves the UI build request against what's
// actually available in the freshly-checked-out worktree, then delegates to
// ApplyWorktreeBuildChoice.
func applyWorktreeBuildRequest(site *config.Site, worktreePath, requested string, log io.Writer) {
	eligible := EligibleBuildReplacers(site, worktreePath)
	optedIn := OptedInBuildReplacers(site, worktreePath)
	scripts := AvailableBuildScripts(worktreePath)
	kind, value := resolveBuildChoice(requested, eligible, optedIn, scripts)

	if requested == "" || requested == "auto" {
		logAutoBuildResolution(log, kind, value)
	}

	choice := worktreeBuildChoice{kind: kind, value: value}
	if kind == "worker" {
		if fw, ok := config.GetFrameworkForDir(site.Framework, site.Path); ok {
			choice.worker = fw.Workers[value]
		}
	}
	ApplyWorktreeBuildChoice(site, worktreePath, choice, log)
}

// logAutoBuildResolution announces what "Automatic" picked and why, so the UI
// modal log makes the decision visible instead of burying it under the
// existing per-kind action line.
func logAutoBuildResolution(log io.Writer, kind, value string) {
	switch kind {
	case "worker":
		logf(log, "Automatic: starting asset worker %q (opted-in via parent .lerd.yaml, replaces_build:true). No `npm run build` will run, the worker serves assets itself.", value)
	case "script":
		logf(log, "Automatic: running `npm run %s` (no asset worker opted in to replace the build).", value)
	case "skip":
		logf(log, "Automatic: nothing to do, no eligible asset worker and no production build script (build/prod/build:prod/build-prod/production) found. First request may throw ViteManifestNotFoundException until you run `npm run dev` or `npm run build`.")
	}
}

func dbChoiceYieldsEmptySchema(choice string) bool {
	return choice == "empty" || choice == "reset"
}

// RemoveWorktreeAndCleanup runs `git worktree remove [--force]` for branch,
// stops its per-worktree worker units, and (when dropDB) drops the isolated
// database and its registry entry. The daemon watcher still handles vhost and
// LAN-share teardown asynchronously. Progress is written to log (may be nil).
func RemoveWorktreeAndCleanup(site *config.Site, branch string, force, dropDB bool, log io.Writer) error {
	wtPath, err := worktreePathForBranch(site, branch)
	if err != nil {
		return fmt.Errorf("worktree %q not found: %w", branch, err)
	}
	wtBase := filepath.Base(wtPath)

	gitArgs := []string{"worktree", "remove"}
	if force {
		gitArgs = append(gitArgs, "--force")
	}
	gitArgs = append(gitArgs, wtPath)
	logf(log, "Running: git %s", strings.Join(gitArgs, " "))
	if err := gitpkg.Run(site.Path, log, gitArgs...); err != nil {
		return fmt.Errorf("git worktree remove: %w", err)
	}

	if err := StopAllWorkersForWorktree(site.Name, wtBase); err != nil {
		logf(log, "[WARN] stopping worktree workers: %v", err)
	}

	if dropDB {
		if entry, ok, _ := config.FindWorktreeDB(site.Name, branch); ok {
			if _, err := DropDatabase(entry.Service, entry.DBName); err != nil {
				logf(log, "[WARN] dropping database %q: %v", entry.DBName, err)
			} else {
				logf(log, "Dropped database %q.", entry.DBName)
			}
			if _, _, err := config.RemoveWorktreeDB(site.Name, branch); err != nil {
				logf(log, "[WARN] removing db registry entry: %v", err)
			}
		}
	}
	logf(log, "Worktree %q removed.", branch)
	return nil
}
