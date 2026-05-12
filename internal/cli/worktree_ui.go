package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
)

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

// WorktreeCheckoutPath returns the directory a new worktree for branch should
// be checked out into: a sibling of the parent site path, "<sitePath>-<slug>".
// If that path already exists it bumps a numeric suffix so we never reuse a
// directory.
func WorktreeCheckoutPath(sitePath, branch string) string {
	base := sitePath + "-" + gitpkg.SanitizeBranch(branch)
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
	if b == "" {
		return false
	}
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+b)
	cmd.Dir = sitePath
	return cmd.Run() == nil
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

// RunWorktreeAdd creates a git worktree for site, waits for lerd's watcher to
// install dependencies, applies the build choice, configures the database, and
// optionally runs migrations. Progress lines are written to log (may be nil).
// Returns the sanitized branch name and the checkout path.
func RunWorktreeAdd(site *config.Site, req WorktreeAddRequest, log io.Writer) (string, string, error) {
	req = normalizeAddRequest(site.Path, req)
	branchInput := req.NewBranch
	if branchInput == "" {
		branchInput = req.ExistingBranch
	}
	branch := gitpkg.SanitizeBranch(branchInput)
	checkoutPath := WorktreeCheckoutPath(site.Path, branchInput)

	gitArgs, err := buildWorktreeAddGitArgs(req, checkoutPath)
	if err != nil {
		return "", "", err
	}
	logf(log, "Running: git %s", strings.Join(gitArgs, " "))
	gitCmd := exec.Command("git", gitArgs...)
	gitCmd.Dir = site.Path
	gitCmd.Stdout = log
	gitCmd.Stderr = log
	if err := gitCmd.Run(); err != nil {
		return "", "", fmt.Errorf("git worktree add: %w", err)
	}

	logf(log, "Waiting for lerd to install dependencies (composer + JS)...")
	if err := WaitForWorktreeReady(checkoutPath, 5*time.Minute); err != nil {
		logf(log, "[WARN] %v, you can finish setup later from the worktree.", err)
	} else {
		logf(log, "Dependencies installed.")
	}

	applyWorktreeBuildRequest(site, checkoutPath, req.Build, log)

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
	return branch, checkoutPath, nil
}

// applyWorktreeBuildRequest resolves the UI build request against what's
// actually available in the freshly-checked-out worktree, then delegates to
// ApplyWorktreeBuildChoice.
func applyWorktreeBuildRequest(site *config.Site, worktreePath, requested string, log io.Writer) {
	eligible := EligibleBuildReplacers(site, worktreePath)
	optedIn := OptedInBuildReplacers(site, worktreePath)
	scripts := AvailableBuildScripts(worktreePath)
	kind, value := resolveBuildChoice(requested, eligible, optedIn, scripts)

	choice := worktreeBuildChoice{kind: kind, value: value}
	if kind == "worker" {
		if fw, ok := config.GetFrameworkForDir(site.Framework, site.Path); ok {
			choice.worker = fw.Workers[value]
		}
	}
	ApplyWorktreeBuildChoice(site, worktreePath, choice, log)
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
	gitCmd := exec.Command("git", gitArgs...)
	gitCmd.Dir = site.Path
	gitCmd.Stdout = log
	gitCmd.Stderr = log
	if err := gitCmd.Run(); err != nil {
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
