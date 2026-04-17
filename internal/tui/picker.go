package tui

import (
	"os/exec"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/config"
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
	m.pickerKind = kindPHP
	m.pickerOptions = versions
	m.pickerCursor = indexOf(versions, s.PHPVersion)
}

// openNodePicker shells out to fnm (same path lerd-ui uses) because node
// version management is fnm's job; lerd doesn't keep its own registry.
// A no-op when fnm reports nothing.
func (m *Model) openNodePicker(s *siteinfo.EnrichedSite) {
	versions := listNodeMajors()
	if len(versions) == 0 {
		m.setStatus("no Node versions installed (run 'lerd node install 20')", 3*time.Second)
		return
	}
	m.pickerKind = kindNode
	m.pickerOptions = versions
	m.pickerCursor = indexOf(versions, s.NodeVersion)
}

// closePicker exits picker mode without applying a choice.
func (m *Model) closePicker() {
	m.pickerKind = kindInfo
	m.pickerOptions = nil
	m.pickerCursor = 0
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
	ver := m.pickerOptions[m.pickerCursor]
	kind := m.pickerKind
	m.closePicker()

	switch kind {
	case kindPHP:
		m.setStatus("switching "+s.Name+" to PHP "+ver+"…", 5*time.Second)
		return runLerd(s.Path, "isolate", ver)
	case kindNode:
		m.setStatus("switching "+s.Name+" to Node "+ver+"…", 5*time.Second)
		return runLerd(s.Path, "isolate:node", ver)
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

func indexOf(ss []string, target string) int {
	for i, s := range ss {
		if s == target {
			return i
		}
	}
	return 0
}
