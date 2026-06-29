package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/sitedoctor"
	"github.com/geodro/lerd/internal/siteinfo"
)

// doctorResultMsg carries a finished doctor run back into the model. site keys
// the result to the site it was run for, so a result that lands after the user
// has moved on to another site is discarded rather than shown against the wrong
// site.
type doctorResultMsg struct {
	site string
	resp sitedoctor.Response
}

// doctorCmd runs the site doctor checks off the main loop. Command and audit
// checks exec in the container (up to ~25s each), so this must never run inline
// in Update — it returns a message the handler folds into the model when done.
func doctorCmd(siteName, fwName, path string) tea.Cmd {
	return func() tea.Msg {
		fw, _ := config.GetFrameworkForDir(fwName, path)
		return doctorResultMsg{site: siteName, resp: sitedoctor.Run(context.Background(), path, fw)}
	}
}

// openDoctorTab switches to the Doctor tab for the focused site and kicks off a
// fresh run when there's no cached result for it yet or the user re-pressed the
// shortcut while already on the tab (an explicit refresh).
func (m *Model) openDoctorTab() tea.Cmd {
	s := m.currentSite()
	if s == nil {
		return nil
	}
	refresh := m.siteTab == tabSiteDoctor
	m.siteTab = tabSiteDoctor
	m.detailScroll = 0
	m.focus = paneDetail
	if m.doctorLoading {
		return nil
	}
	if refresh || m.doctorSite != s.Name {
		m.doctorLoading = true
		m.doctorChecks = nil
		m.doctorSite = s.Name
		return doctorCmd(s.Name, s.FrameworkName, s.Path)
	}
	return nil
}

// doctorStatusVisual maps a check status to its style, dot glyph, and label in
// one switch so the Doctor panel's glyph and word can't disagree, reading like
// the rest of the TUI: green pass, amber warn, red fail, grey unknown (a check
// that couldn't run, e.g. the app is down).
func doctorStatusVisual(status string) (style lipgloss.Style, glyph, label string) {
	switch status {
	case sitedoctor.StatusOK:
		return runningStyle, glyphRunning, "ok"
	case sitedoctor.StatusWarn:
		return suspendedStyle, glyphSuspended, "warn"
	case sitedoctor.StatusFail:
		return failingStyle, glyphFailing, "fail"
	default:
		return stoppedStyle, glyphStopped, "unknown"
	}
}

// siteDoctorContentLines renders the focused site's doctor panel: a running
// placeholder while the checks are in flight, then one row per check with its
// status, detail, and suggested fix command. Read-only — the panel names the fix
// (key:generate, migrate, …) rather than running it, so a stray keypress can't
// migrate a database from a status view.
func siteDoctorContentLines(m *Model, site *siteinfo.EnrichedSite, innerW int) []string {
	out := make([]string, 0, 32)
	out = append(out, renderSiteTabHeader(tabSiteDoctor, innerW, availableSiteTabs(site))...)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	if site == nil {
		add(dimStyle.Render("  no site selected"))
		return out
	}

	add(sectionStyle.Render("Doctor") + dimStyle.Render("  ·  "+site.PrimaryDomain()))
	add("")

	if m.doctorLoading && m.doctorSite == site.Name {
		add(dimStyle.Render("  running checks… (migrations may take a moment)"))
		return out
	}
	if m.doctorSite != site.Name {
		add(dimStyle.Render("  press ") + accentStyle.Render("5") + dimStyle.Render(" to run the checks"))
		return out
	}
	if len(m.doctorChecks) == 0 {
		add(dimStyle.Render("  no checks applied to this site"))
		return out
	}

	failures, warnings := 0, 0
	for _, c := range m.doctorChecks {
		switch c.Status {
		case sitedoctor.StatusFail:
			failures++
		case sitedoctor.StatusWarn:
			warnings++
		}
	}
	switch {
	case failures > 0 || warnings > 0:
		add(dimStyle.Render(fmt.Sprintf("  %d failing · %d warning", failures, warnings)))
	default:
		add(runningStyle.Render("  all checks pass"))
	}
	add("")

	for _, c := range m.doctorChecks {
		name := c.Label
		if name == "" {
			name = strings.ReplaceAll(c.Name, "_", " ")
		}
		st, glyph, label := doctorStatusVisual(c.Status)
		add(fmt.Sprintf("  %s %s  %s", st.Render(glyph), padRight(name, 14), st.Render(label)))
		if c.Detail != "" {
			add(dimStyle.Render("      " + c.Detail))
		}
		if c.Fix != "" {
			add(dimStyle.Render("      fix: ") + accentStyle.Render(c.Fix))
		}
	}
	add("")
	add(dimStyle.Render("  press ") + accentStyle.Render("5") + dimStyle.Render(" to re-run"))
	return out
}
