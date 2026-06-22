package tui

import (
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/config"
	nodeDet "github.com/geodro/lerd/internal/node"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/siteinfo"
)

// openPHPPicker loads installed PHP versions and enters picker mode on the
// detail pane. A no-op when no versions are installed.
func (m *Model) openPHPPicker(s *siteinfo.EnrichedSite) {
	versions, err := phpPkg.ListInstalled()
	if err != nil || len(versions) == 0 {
		m.setStatus("no PHP versions installed", 3*time.Second)
		return
	}
	versions = frankenPHPRunnable(s.Runtime, versions, s.PHPVersion)
	m.pickerKind = kindPHP
	m.pickerOptions = versions
	m.pickerDisabled = phpDisabledMask(versions, s.FrameworkPHPMin, s.FrameworkPHPMax, s.PHPVersion)
	m.pickerCursor = firstEnabledFrom(indexOf(versions, s.PHPVersion), m.pickerDisabled)
}

// openNodePicker shells out to fnm (same path lerd-ui uses) because node
// version management is fnm's job; lerd doesn't keep its own registry.
// A no-op when fnm reports nothing.
func (m *Model) openNodePicker(s *siteinfo.EnrichedSite) {
	versions := listNodeMajors()
	bunAvailable := nodeDet.BunPath() != ""
	if len(versions) == 0 && !bunAvailable {
		m.setStatus("no Node versions installed (run 'lerd node install 20')", 3*time.Second)
		return
	}
	// bun is a JS-runtime toggle rather than a Node version, so it joins the
	// list as a project-level pin (main site only, never per-worktree) when a
	// host bun exists, mirroring the web Node dropdown.
	if bunAvailable {
		versions = append(versions, "bun")
	}
	m.pickerKind = kindNode
	m.pickerOptions = versions
	m.pickerDisabled = nil
	if nodeDet.JSRuntime(s.Path) == "bun" {
		m.pickerCursor = indexOf(versions, "bun")
	} else {
		m.pickerCursor = indexOf(versions, s.NodeVersion)
	}
}

// openWorktreePHPPicker mirrors openPHPPicker but scopes the apply to the
// worktree's checkout via pickerWorktreePath, so the resulting .php-version
// is written inside the worktree rather than the parent site.
func (m *Model) openWorktreePHPPicker(s *siteinfo.EnrichedSite, row detailRow) {
	wt := findWorktree(s, row.branch)
	if wt == nil {
		return
	}
	versions, err := phpPkg.ListInstalled()
	if err != nil || len(versions) == 0 {
		m.setStatus("no PHP versions installed", 3*time.Second)
		return
	}
	versions = frankenPHPRunnable(s.Runtime, versions, wt.PHPVersion)
	m.pickerKind = kindWorktreePHP
	m.pickerOptions = versions
	// A worktree shares the parent site's framework, so it inherits its range.
	m.pickerDisabled = phpDisabledMask(versions, s.FrameworkPHPMin, s.FrameworkPHPMax, wt.PHPVersion)
	m.pickerCursor = firstEnabledFrom(indexOf(versions, wt.PHPVersion), m.pickerDisabled)
	m.pickerWorktreePath = row.branchPath
	m.pickerWorktreeName = row.branch
}

// openWorktreeNodePicker is the Node analogue of openWorktreePHPPicker.
func (m *Model) openWorktreeNodePicker(s *siteinfo.EnrichedSite, row detailRow) {
	wt := findWorktree(s, row.branch)
	if wt == nil {
		return
	}
	versions := listNodeMajors()
	if len(versions) == 0 {
		m.setStatus("no Node versions installed (run 'lerd node install 20')", 3*time.Second)
		return
	}
	m.pickerKind = kindWorktreeNode
	m.pickerOptions = versions
	m.pickerDisabled = nil
	m.pickerCursor = indexOf(versions, wt.NodeVersion)
	m.pickerWorktreePath = row.branchPath
	m.pickerWorktreeName = row.branch
}

// pickerIsDisabled reports whether the option at index i is disabled (an
// out-of-range PHP version that can't be selected).
func (m *Model) pickerIsDisabled(i int) bool {
	return i >= 0 && i < len(m.pickerDisabled) && m.pickerDisabled[i]
}

// closePicker exits picker mode without applying a choice.
func (m *Model) closePicker() {
	m.pickerKind = kindInfo
	m.pickerOptions = nil
	m.pickerDisabled = nil
	m.pickerCursor = 0
	m.pickerWorktreePath = ""
	m.pickerWorktreeName = ""
}

// applyPicker runs `lerd isolate` or `lerd isolate:node` for the selected
// version, then closes the picker. Refresh will land shortly via the regular
// ActionResult → loadCmd path.
func (m *Model) applyPicker() tea.Cmd {
	if m.pickerKind == kindInfo || len(m.pickerOptions) == 0 {
		return nil
	}
	s := m.currentSite()
	if s == nil {
		m.closePicker()
		return nil
	}
	if m.pickerCursor >= len(m.pickerOptions) {
		m.pickerCursor = len(m.pickerOptions) - 1
	}
	// A disabled (out-of-range) entry can't be applied; ignore the keystroke
	// and leave the picker open so the user can pick a valid version.
	if m.pickerIsDisabled(m.pickerCursor) {
		return nil
	}
	ver := m.pickerOptions[m.pickerCursor]
	kind := m.pickerKind
	m.closePicker()

	switch kind {
	case kindPHP:
		m.setStatus("switching "+s.Name+" to PHP "+ver+"…", 5*time.Second)
		return runLerd(s.Path, "isolate", ver)
	case kindNode:
		if ver == "bun" {
			m.setStatus("switching "+s.Name+" to bun…", 5*time.Second)
		} else {
			m.setStatus("switching "+s.Name+" to Node "+ver+"…", 5*time.Second)
		}
		var cmds []tea.Cmd
		for _, a := range nodePickerArgs(ver, nodeDet.UsesBun(s.Path)) {
			cmds = append(cmds, runLerd(s.Path, a...))
		}
		return tea.Sequence(cmds...)
	case kindWorktreePHP:
		path, branch := m.pickerWorktreePath, m.pickerWorktreeName
		m.pickerWorktreePath, m.pickerWorktreeName = "", ""
		m.setStatus("switching "+branch+" to PHP "+ver+"…", 5*time.Second)
		return runLerd(path, "isolate", ver)
	case kindWorktreeNode:
		path, branch := m.pickerWorktreePath, m.pickerWorktreeName
		m.pickerWorktreePath, m.pickerWorktreeName = "", ""
		m.setStatus("switching "+branch+" to Node "+ver+"…", 5*time.Second)
		return runLerd(path, "isolate:node", ver)
	}
	return nil
}

// listNodeMajors returns the installed Node major versions as reported by
// fnm. Mirrors ui.handleNodeVersions so the picker sees the same list the
// web UI dropdown shows.
func listNodeMajors() []string {
	fnm := config.BinDir() + "/fnm"
	out, err := exec.Command(fnm, "list").Output()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var versions []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "* "))
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		v := strings.TrimPrefix(fields[0], "v")
		if v == "" {
			continue
		}
		major := strings.SplitN(v, ".", 2)[0]
		if seen[major] || strings.Trim(major, "0123456789") != "" {
			continue
		}
		seen[major] = true
		versions = append(versions, major)
	}
	sort.Strings(versions)
	return versions
}

// nodePickerArgs maps a Node-picker choice to the lerd command(s) to run.
// "bun" pins the JS runtime; a real Node version pins the version, first forcing
// Node when the site currently resolves to bun so the dev/Vite worker actually
// switches off bun rather than ignoring the chosen version. currentlyBun is the
// effective runtime (node.UsesBun), so it covers an explicit pin and a
// lockfile auto-detect alike.
func nodePickerArgs(ver string, currentlyBun bool) [][]string {
	if ver == "bun" {
		return [][]string{{"js:runtime", "bun"}}
	}
	if currentlyBun {
		// The site currently resolves to bun — whether from an explicit pin or
		// a lockfile auto-detect — so pin Node before isolate:node, or the
		// dev/Vite worker keeps running bun and ignores the chosen version.
		return [][]string{{"js:runtime", "node"}, {"isolate:node", ver}}
	}
	return [][]string{{"isolate:node", ver}}
}

// frankenPHPRunnable narrows the installed PHP versions to the set
// dunglas/frankenphp publishes an image for when the site runs under FrankenPHP,
// since FrankenPHP can only boot those and picking another silently downgrades.
// The current version is always kept so the picker never goes blank, and a
// non-FrankenPHP site (or a filter that would empty the list) is left untouched.
func frankenPHPRunnable(runtime string, versions []string, current string) []string {
	if runtime != "frankenphp" {
		return versions
	}
	out := versions[:0:0]
	for _, v := range versions {
		if config.IsFrankenPHPVersion(v) || v == current {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return versions
	}
	return out
}

// phpDisabledMask returns a slice parallel to versions marking those outside
// the framework's [min, max] range as disabled. The current version is never
// disabled, so the active selection always shows. An empty min and max (no
// framework range, or a guessed version) disables nothing.
func phpDisabledMask(versions []string, min, max, current string) []bool {
	mask := make([]bool, len(versions))
	if min == "" && max == "" {
		return mask
	}
	for i, v := range versions {
		if v == current {
			continue
		}
		if (min != "" && phpPkg.CompareMajorMinor(v, min) < 0) || (max != "" && phpPkg.CompareMajorMinor(v, max) > 0) {
			mask[i] = true
		}
	}
	return mask
}

// firstEnabledFrom returns start if enabled, otherwise the nearest enabled index
// scanning forward then wrapping to the top. Returns start when all are disabled.
func firstEnabledFrom(start int, disabled []bool) int {
	if start < 0 || start >= len(disabled) {
		start = 0
	}
	if len(disabled) == 0 || !disabled[start] {
		return start
	}
	for i := start; i < len(disabled); i++ {
		if !disabled[i] {
			return i
		}
	}
	for i := 0; i < start; i++ {
		if !disabled[i] {
			return i
		}
	}
	return start
}

func indexOf(ss []string, target string) int {
	for i, s := range ss {
		if s == target {
			return i
		}
	}
	return 0
}
