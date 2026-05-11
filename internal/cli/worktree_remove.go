package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/spf13/cobra"
)

// newWorktreeRemoveCmd is the `lerd worktree remove` subcommand. Runs
// `git worktree remove`, then waits briefly for the watcher's cleanup hook
// to drop the vhost, isolated DB, and LAN share entry. Surfaces a confirm
// prompt when the worktree has lerd-managed state attached so the user
// understands what's about to be torn down.
func newWorktreeRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                "remove [git-worktree-remove args...]",
		Short:              "Remove a git worktree (any git flags) and its lerd-managed state",
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

			// Best-effort: identify the branch about to be removed so we can
			// warn about lerd-managed state. We pick the last positional arg
			// (git's <worktree> argument) and resolve its current HEAD.
			branch := guessBranchFromArgs(cwd, args)
			if branch != "" {
				if err := confirmRemovalIfManaged(site, branch); err != nil {
					return err
				}
			}

			// Snapshot the worktree dir basename before git removes the
			// worktree — once the directory is gone we can't derive it
			// reliably from the user's positional arg.
			var wtBase string
			if last := lastNonFlag(args); last != "" {
				wtBase = filepath.Base(last)
			}

			if err := runGitWorktreeRemove(args); err != nil {
				return err
			}

			// Stop any per-worktree worker units before they restart-loop
			// against the now-deleted WorkingDirectory. Best-effort — a
			// failure here is a warning, not a hard error.
			if wtBase != "" {
				if err := StopAllWorkersForWorktree(site.Name, wtBase); err != nil {
					fmt.Printf("[WARN] stopping worktree workers: %v\n", err)
				}
			}

			if branch != "" {
				if err := promptDeleteIsolatedDB(site, branch); err != nil {
					fmt.Printf("[WARN] DB cleanup skipped: %v\n", err)
				}
			}

			if branch != "" {
				if err := waitForWorktreeCleanup(site.Name, branch, 30*time.Second); err != nil {
					fmt.Printf("[WARN] %v\n", err)
				} else {
					fmt.Println("Worktree removed and lerd state cleaned up.")
				}
			}
			return nil
		},
	}
	return cmd
}

// promptDeleteIsolatedDB asks whether to drop the worktree's isolated
// database now that the worktree itself is gone. Default is to keep the
// database — the safer choice if the user hasn't backed it up. Skipping
// here preserves the registry entry too, so re-adding the worktree later
// reconnects to the existing data.
func promptDeleteIsolatedDB(site *config.Site, branch string) error {
	entry, ok, err := config.FindWorktreeDB(site.Name, branch)
	if err != nil || !ok {
		return nil
	}

	var picked string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(fmt.Sprintf("Delete isolated database %q in service %q?", entry.DBName, entry.Service)).
			Description("Skipping keeps the database and the registry entry, so a future `lerd worktree add` of this branch reuses the same data.").
			Options(
				huh.NewOption("Keep the database", ""),
				huh.NewOption(fmt.Sprintf("Drop %q (data is gone)", entry.DBName), "drop"),
			).
			Value(&picked),
	))
	if err := form.Run(); err != nil {
		return err
	}
	if picked != "drop" {
		return nil
	}
	if _, err := DropDatabase(entry.Service, entry.DBName); err != nil {
		return fmt.Errorf("dropping %q: %w", entry.DBName, err)
	}
	if _, _, err := config.RemoveWorktreeDB(site.Name, branch); err != nil {
		return fmt.Errorf("removing registry entry: %w", err)
	}
	fmt.Printf("Dropped database %q.\n", entry.DBName)
	return nil
}

// runGitWorktreeRemove invokes git, capturing stderr so it can detect git's
// "use --force" hint when the worktree has modifications. On that specific
// error it prompts the user to retry with --force prepended; any other
// failure is returned verbatim. stderr is mirrored to the user's terminal
// so progress and warnings are still visible during the first attempt.
func runGitWorktreeRemove(args []string) error {
	if hasForceFlag(args) {
		return runGit(append([]string{"worktree", "remove"}, args...), true)
	}

	gitArgs := append([]string{"worktree", "remove"}, args...)
	fmt.Printf("Running: git %s\n", strings.Join(gitArgs, " "))
	cmd := exec.Command("git", gitArgs...)
	cmd.Stdout = os.Stdout
	stderrBuf := &strings.Builder{}
	cmd.Stderr = io.MultiWriter(os.Stderr, stderrBuf)
	if err := cmd.Run(); err == nil {
		return nil
	}

	if !strings.Contains(stderrBuf.String(), "--force") {
		return fmt.Errorf("git worktree remove: exit status from git")
	}

	var picked string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Worktree has modified or untracked files. Force-remove and discard them?").
			Options(
				huh.NewOption("Cancel — keep the worktree as-is", ""),
				huh.NewOption("Force remove (discards modifications)", "force"),
			).
			Value(&picked),
	))
	if err := form.Run(); err != nil {
		return err
	}
	if picked != "force" {
		return fmt.Errorf("aborted")
	}
	return runGit(append([]string{"worktree", "remove", "--force"}, args...), true)
}

// lastNonFlag returns the trailing positional argument from a git command,
// which is the worktree path or basename. Returns "" if every arg is a flag.
func lastNonFlag(args []string) string {
	for i := len(args) - 1; i >= 0; i-- {
		if !strings.HasPrefix(args[i], "-") {
			return args[i]
		}
	}
	return ""
}

func hasForceFlag(args []string) bool {
	for _, a := range args {
		if a == "-f" || a == "--force" {
			return true
		}
	}
	return false
}

func runGit(args []string, mirrorOutput bool) error {
	fmt.Printf("Running: git %s\n", strings.Join(args, " "))
	cmd := exec.Command("git", args...)
	if mirrorOutput {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", args[1], err)
	}
	return nil
}

// guessBranchFromArgs walks the user's git args looking for the last
// non-flag token, treats it as the worktree path or branch name, and reads
// HEAD from the matching .git/worktrees/<name>/HEAD file. Returns "" when
// nothing usable is found — the wrapper still runs git, just without the
// confirm-prompt + cleanup-poll niceties.
func guessBranchFromArgs(sitePath string, args []string) string {
	candidate := ""
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		candidate = a
	}
	if candidate == "" {
		return ""
	}
	// `git worktree remove <name>` accepts either the worktree path or just
	// the directory basename; lerd's watcher keys off basename-derived
	// branches, so reading <site>/.git/worktrees/<basename>/HEAD covers
	// both forms in practice.
	wtMeta := filepath.Join(sitePath, ".git", "worktrees", filepath.Base(candidate))
	headData, err := os.ReadFile(filepath.Join(wtMeta, "HEAD"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(headData))
	if strings.HasPrefix(line, "ref: refs/heads/") {
		return gitpkg.SanitizeBranch(strings.TrimPrefix(line, "ref: refs/heads/"))
	}
	return ""
}

// confirmRemovalIfManaged shows the user what lerd-managed state is about
// to be torn down (LAN share) so they can cancel before git runs. Isolated
// databases are NOT mentioned here — the wrapper asks about them explicitly
// at the end, after git has succeeded. No prompt when nothing's attached.
func confirmRemovalIfManaged(site *config.Site, branch string) error {
	var bullets []string
	if e, ok, _ := config.FindWorktreeLAN(site.Name, branch); ok {
		bullets = append(bullets, fmt.Sprintf("LAN share on port %d (will be released)", e.Port))
	}
	if len(bullets) == 0 {
		return nil
	}

	prompt := "Remove worktree " + branch + "?\n\nThis will also tear down:\n  - " + strings.Join(bullets, "\n  - ")
	var picked string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(prompt).
			Options(
				huh.NewOption("Cancel — keep the worktree and its lerd state", ""),
				huh.NewOption("Remove the worktree", "remove"),
			).
			Value(&picked),
	))
	if err := form.Run(); err != nil {
		return err
	}
	if picked != "remove" {
		return fmt.Errorf("aborted by user")
	}
	return nil
}

// waitForWorktreeCleanup blocks until the watcher has dropped the LAN-share
// entry (the only async cleanup left after the DB-prompt change). Returns
// nil if there's nothing to wait for.
func waitForWorktreeCleanup(siteName, branch string, deadline time.Duration) error {
	if _, ok, _ := config.FindWorktreeLAN(siteName, branch); !ok {
		return nil
	}
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if _, ok, _ := config.FindWorktreeLAN(siteName, branch); !ok {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("LAN-share cleanup did not complete within %s — daemon will sweep the entry on next start", deadline)
}
