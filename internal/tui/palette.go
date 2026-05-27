package tui

import (
	"os"
	"os/exec"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Command palette: press `:` (vim-style) anywhere to open an inline prompt
// for an arbitrary lerd subcommand. The user types `service restart redis`
// and hits enter; we shell out exactly as if they'd typed `lerd service
// restart redis` in a terminal. Mirrors the web UI's CommandRunModal /
// CommandPalette without requiring focus on any particular pane.

// paletteCommands is the curated list of command paths the palette
// autocompletes against. Hardcoded rather than walked off cobra at runtime
// to avoid an import cycle (cli already imports tui via NewTuiCmd). Keep
// this list synced with cmd/lerd/main.go's AddCommand calls when adding
// new top-level verbs; sub-command depth is deliberately shallow because
// the goal is discovery, not exhaustive completion.
var paletteCommands = []string{
	"about",
	"autostart enable",
	"autostart disable",
	"bug-report",
	"check",
	"composer",
	"console",
	"db create",
	"db export",
	"db import",
	"db restore",
	"db shell",
	"db snapshot",
	"db snapshots",
	"db:isolate",
	"db:share",
	"doctor",
	"domain add",
	"domain remove",
	"dump on",
	"dump off",
	"dump status",
	"dump tail",
	"dump clear",
	"env",
	"env check",
	"env restore",
	"fetch",
	"framework update",
	"horizon start",
	"horizon stop",
	"import",
	"init",
	"install",
	"isolate",
	"isolate:node",
	"lan share",
	"lan unshare",
	"lan expose on",
	"lan expose off",
	"lan status",
	"link",
	"logs",
	"mcp",
	"new",
	"node:install",
	"node:remove",
	"node:use",
	"notify on",
	"notify off",
	"notify status",
	"open",
	"park",
	"pause",
	"php:list",
	"php:rebuild",
	"profile on",
	"profile off",
	"profile status",
	"profile open",
	"profile clear",
	"queue start",
	"queue stop",
	"rebuild",
	"remote-control on",
	"remote-control off",
	"remote-control status",
	"restart",
	"reverb start",
	"reverb stop",
	"sail",
	"schedule start",
	"schedule stop",
	"secure",
	"service restart",
	"service rollback",
	"service start",
	"service stop",
	"service update",
	"sites",
	"start",
	"status",
	"stop",
	"test",
	"unlink",
	"unpark",
	"unpause",
	"unsecure",
	"update",
	"use",
	"watch",
	"worker heal",
	"worker start",
	"worker stop",
	"workers mode",
	"worktree add",
	"worktree remove",
	"xdebug on",
	"xdebug off",
}

// openPalette switches to palette-input mode. We don't pin focus to a
// particular pane — the prompt sits above the footer and is dismissed by
// esc, so the user returns to whatever they were doing.
func (m *Model) openPalette() {
	m.paletteActive = true
	m.paletteInput = ""
}

// handlePaletteKey collects characters for the palette input, commits on
// enter (shells `lerd <args>`), cancels on esc, completes on tab. Mirrors
// the shape of the other modal-key handlers (handleFilterKey,
// handleDomainInputKey) so adding a future history (`↑` / `↓`) only
// touches this one function.
func (m *Model) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.paletteActive = false
		m.paletteInput = ""
		return m, nil
	case "enter":
		raw := strings.TrimSpace(m.paletteInput)
		m.paletteActive = false
		m.paletteInput = ""
		if raw == "" {
			return m, nil
		}
		args := parsePaletteArgs(raw)
		if len(args) == 0 {
			return m, nil
		}
		return m, runPaletteCommand(raw, args)
	case "tab":
		m.paletteInput = completePaletteInput(m.paletteInput, paletteCommands)
		return m, nil
	case "ctrl+c":
		m.logTail.Stop()
		return m, tea.Quit
	case "backspace":
		if len(m.paletteInput) > 0 {
			r := []rune(m.paletteInput)
			m.paletteInput = string(r[:len(r)-1])
		}
	default:
		if len(msg.Runes) > 0 {
			m.paletteInput += string(msg.Runes)
		}
	}
	return m, nil
}

// paletteSuggestions returns at most max command paths that start with the
// current input (case-insensitive). Sorted alphabetically so the user sees
// a stable list as they type. Empty input returns the empty list — we don't
// dump the whole catalog by default; tab gives them the first match.
func paletteSuggestions(input string, all []string, max int) []string {
	needle := strings.TrimSpace(strings.ToLower(input))
	if needle == "" {
		return nil
	}
	out := make([]string, 0, max)
	for _, c := range all {
		if strings.HasPrefix(c, needle) {
			out = append(out, c)
			if len(out) >= max {
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// completePaletteInput returns the new palette input after a tab press.
// If exactly one suggestion matches, complete to it with a trailing space
// so the user can keep typing arguments. If more than one matches,
// complete to their longest common prefix so subsequent tabs make
// progress instead of cycling. No match: leave the input unchanged.
func completePaletteInput(input string, all []string) string {
	matches := paletteSuggestions(input, all, 32)
	switch len(matches) {
	case 0:
		return input
	case 1:
		return matches[0] + " "
	default:
		lcp := longestCommonPrefix(matches)
		if len(lcp) > len(input) {
			return lcp
		}
		return input
	}
}

// longestCommonPrefix returns the longest shared start across all strings.
// Used by completePaletteInput so a tab press makes the input as long as
// the unambiguous prefix permits — e.g. "se" against ["service start",
// "service stop", "secure"] becomes "se", but "ser" becomes "service ".
func longestCommonPrefix(in []string) string {
	if len(in) == 0 {
		return ""
	}
	prefix := in[0]
	for _, s := range in[1:] {
		for !strings.HasPrefix(s, prefix) {
			if prefix == "" {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// runPaletteCommand suspends the bubbletea program and runs `lerd <args>`
// with the user's real terminal attached, so they see the full streamed
// output exactly as a manual invocation would print it. After the
// subprocess exits we run a short shell pause so quick commands don't
// flash back to the TUI before the user can read the result; pressing
// enter (or any line) returns to the dashboard. The status bar then
// records a one-line summary so the user has lasting feedback even if
// they want to refer back to the most recent action.
func runPaletteCommand(raw string, args []string) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		self = "lerd"
	}
	// Build a sh -c invocation so we can append a portable "press enter"
	// pause without re-implementing a TTY waiter in Go. The bubbletea
	// program is already suspended (tea.ExecProcess hands the terminal
	// back), so plain `read` reads from the user's tty.
	script := shQuote(self) + " " + shQuoteAll(args) +
		`; status=$?; printf '\n[press enter to return to lerd tui] '; read _; exit $status`
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return ActionResult{Summary: "lerd " + raw, Err: err, Detail: err.Error()}
		}
		return ActionResult{Summary: "lerd " + raw}
	})
}

// shQuote wraps s in single quotes for a POSIX shell, escaping any embedded
// single quote with the standard `'\”` pattern. Used so the executable
// path and each argument survive shell parsing untouched even when they
// contain spaces or punctuation.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func shQuoteAll(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shQuote(a)
	}
	return strings.Join(quoted, " ")
}

// parsePaletteArgs splits a raw command line on whitespace, treating
// adjacent runs of spaces as one separator. We deliberately do not honour
// shell quoting: the TUI doesn't expand globs or run pipes, and the
// underlying CLI uses cobra which already expects argv-style splits.
// Anyone needing a true shell can drop to `t` and run the command there.
func parsePaletteArgs(s string) []string {
	return strings.Fields(s)
}
