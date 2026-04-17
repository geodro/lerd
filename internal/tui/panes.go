package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/geodro/lerd/internal/siteinfo"
)

// View implements tea.Model.
func (m *Model) View() string {
	if m.width < 60 || m.height < 12 {
		return "terminal too small (need at least 60×12)\n"
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	statusBar := m.renderStatus()

	reserved := lipgloss.Height(header) + lipgloss.Height(footer)
	if statusBar != "" {
		reserved += lipgloss.Height(statusBar)
	}
	bodyH := m.height - reserved
	if bodyH < 6 {
		bodyH = 6
	}

	// Logs pane gets at least half the full window when open. Clamp so the
	// top lists always keep at least 6 rows to stay useful.
	logH := 0
	if m.showLogs {
		logH = m.height / 2
		if half := bodyH / 2; logH < half {
			logH = half
		}
		if logH < 10 {
			logH = 10
		}
		if logH > bodyH-6 {
			logH = bodyH - 6
		}
	}
	topH := bodyH - logH
	if topH < 6 {
		topH = 6
	}

	// Left column stacks sites on top of services; right column is the
	// site detail (full topH). When services is hidden, sites takes the
	// whole left column.
	leftW := m.width * 2 / 5
	if leftW < 36 {
		leftW = 36
	}
	if leftW > m.width-30 {
		leftW = m.width - 30
	}
	rightW := m.width - leftW

	var left string
	if m.hideServices {
		left = m.renderSites(leftW, topH)
	} else {
		svcNeeded := len(m.snap.Services) + 3
		if svcNeeded < 6 {
			svcNeeded = 6
		}
		svcH := svcNeeded
		if lim := topH / 2; svcH > lim {
			svcH = lim
		}
		if svcH > topH-6 {
			svcH = topH - 6
		}
		if svcH < 4 {
			svcH = 4
		}
		siteH := topH - svcH
		sites := m.renderSites(leftW, siteH)
		services := m.renderServices(leftW, svcH)
		left = lipgloss.JoinVertical(lipgloss.Left, sites, services)
	}
	detail := m.renderDetailInline(rightW, topH, m.focus == paneDetail)
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, detail)

	sections := []string{header, top}
	if m.showLogs {
		sections = append(sections, m.renderLogs(m.width, logH))
	}
	if statusBar != "" {
		sections = append(sections, statusBar)
	}
	sections = append(sections, footer)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *Model) renderHeader() string {
	parts := []string{titleStyle.Render("lerd " + m.version)}

	if m.updateAvailable != "" {
		parts = append(parts, accentStyle.Render("update: "+m.updateAvailable+" (run `lerd update`)"))
	}

	if m.snap.Status.DNSOk {
		parts = append(parts, runningStyle.Render("DNS ok"))
	} else {
		parts = append(parts, failingStyle.Render("DNS down"))
	}

	if m.snap.Status.NginxRunning {
		parts = append(parts, runningStyle.Render("nginx up"))
	} else {
		parts = append(parts, stoppedStyle.Render("nginx down"))
	}

	if len(m.snap.Status.PHPRunning) > 0 {
		parts = append(parts, accentStyle.Render("FPM "+strings.Join(m.snap.Status.PHPRunning, ",")))
	}

	if m.snap.Status.WatcherRunning {
		parts = append(parts, dimStyle.Render("watcher"))
	}

	parts = append(parts, dimStyle.Render(time.Now().Format("15:04:05")))

	return strings.Join(parts, "  ·  ")
}

func (m *Model) renderFooter() string {
	if m.filterActive {
		return helpStyle.Render("  filter: type to match · enter apply · esc clear")
	}
	keys := []string{
		"tab panes",
		"↑↓ nav",
		"space toggle",
		"/ filter",
		"o sort",
		"s start",
		"x stop",
		"r restart",
		"l logs",
		"t shell",
		"v services",
		"S settings",
		"? help",
		"q quit",
	}
	return helpStyle.Render("  " + strings.Join(keys, "   "))
}

func (m *Model) renderStatus() string {
	if m.status == "" {
		return ""
	}
	if !m.statusExpiry.IsZero() && time.Now().After(m.statusExpiry) {
		m.status = ""
		return ""
	}
	return helpStyle.Render("  " + m.status)
}

func (m *Model) renderSites(w, h int) string {
	style := paneStyle(m.focus == paneSites)
	innerW, innerH := innerSize(style, w, h)

	sites := m.visibleSites()
	total := len(m.snap.Sites)
	title := fmt.Sprintf("Sites (%d/%d · sort: %s)", len(sites), total, m.siteSort.label())
	lines := []string{padToWidth(clipLine(sectionStyle.Render(title), innerW), innerW)}

	// Filter bar appears as a second header row whenever the user has
	// entered any filter text or is currently typing. Keeps the active
	// filter visible at a glance and distinguishes "empty list because no
	// matches" from "empty list because nothing was linked yet".
	activeFilter := m.focus == paneSites && m.filterActive
	if activeFilter || m.siteFilter != "" {
		lines = append(lines, padToWidth(filterBar(m.siteFilter, activeFilter), innerW))
	}

	availRows := innerH - len(lines)
	if availRows < 1 {
		availRows = 1
	}

	switch {
	case total == 0:
		lines = append(lines, padToWidth(dimStyle.Render("no linked sites"), innerW))
	case len(sites) == 0:
		lines = append(lines, padToWidth(dimStyle.Render("no sites match filter"), innerW))
	default:
		rows := make([]string, 0, len(sites))
		for i, s := range sites {
			row := renderSiteRow(i == m.siteCursor && m.focus == paneSites, s, innerW)
			rows = append(rows, padToWidth(clipLine(row, innerW), innerW))
		}
		visible := viewport(rows, m.siteCursor, availRows, &m.siteScroll)
		lines = append(lines, visible...)
	}
	for len(lines) < innerH {
		lines = append(lines, spaces(innerW))
	}

	return style.Render(strings.Join(lines, "\n"))
}

// siteWorkerColWidth is the fixed display-width reservation for the worker
// glyphs column in the sites list. Needs to be consistent across rows for
// the PHP column (which sits to the left of it) to align cleanly.
// 12 cells fits the typical worst case: q·s·v·h·m·m (6 glyphs + 5 spaces).
const siteWorkerColWidth = 12

func renderSiteRow(selected bool, s siteinfo.EnrichedSite, paneW int) string {
	glyph := fpmGlyph(s)
	workers := workerGlyphs(s)

	// Display the primary domain (the URL users actually visit) rather than
	// the internal site registry name — the name is still used for command
	// dispatch and filtering, it just isn't what shows up in the list.
	name := s.PrimaryDomain()
	if name == "" {
		name = s.Name
	}
	if s.Paused {
		name += " (paused)"
	}

	php := s.PHPVersion
	if php == "" && s.ContainerPort > 0 {
		php = "custom"
	}

	// Reserve the SAME budget on every row so PHP and worker columns line
	// up vertically, regardless of which workers a site happens to run.
	// The previous version subtracted workersW per row, which left empty-
	// worker rows with a wider name column and shifted PHP leftward.
	reserved := 4 /* prefix + glyph + spaces */ + 7 /* php %-7s */ + 1 + siteWorkerColWidth
	nameW := paneW - reserved
	if nameW < 16 {
		nameW = 16
	}
	name = padRight(truncatePlain(name, nameW), nameW)

	prefix := " "
	if selected {
		prefix = accentStyle.Render("▸")
	}

	styled := name
	switch {
	case s.Paused:
		styled = pausedStyle.Render(name)
	case selected:
		styled = selectedStyle.Render(name)
	}

	return fmt.Sprintf("%s %s %s %-7s %s", prefix, glyph, styled, php, padToWidth(workers, siteWorkerColWidth))
}

func fpmGlyph(s siteinfo.EnrichedSite) string {
	if s.Paused {
		return pausedStyle.Render(glyphPaused)
	}
	if s.FPMRunning {
		return runningStyle.Render(glyphRunning)
	}
	return stoppedStyle.Render(glyphStopped)
}

func workerGlyphs(s siteinfo.EnrichedSite) string {
	var out []string
	add := func(has, running, failing bool, label string) {
		if !has {
			return
		}
		switch {
		case failing:
			out = append(out, failingStyle.Render(label))
		case running:
			out = append(out, runningStyle.Render(label))
		default:
			out = append(out, stoppedStyle.Render(label))
		}
	}
	add(s.HasQueueWorker, s.QueueRunning, s.QueueFailing, "q")
	add(s.HasScheduleWorker, s.ScheduleRunning, s.ScheduleFailing, "s")
	add(s.HasReverb, s.ReverbRunning, s.ReverbFailing, "v")
	add(s.HasHorizon, s.HorizonRunning, s.HorizonFailing, "h")
	for _, fw := range s.FrameworkWorkers {
		add(true, fw.Running, fw.Failing, "•")
	}
	return strings.Join(out, " ")
}

func (m *Model) renderServices(w, h int) string {
	style := paneStyle(m.focus == paneServices)
	innerW, innerH := innerSize(style, w, h)

	services := m.visibleServices()
	total := len(m.snap.Services)
	title := fmt.Sprintf("Services (%d/%d · sort: %s)", len(services), total, m.svcSort.label())
	lines := []string{padToWidth(clipLine(sectionStyle.Render(title), innerW), innerW)}

	activeFilter := m.focus == paneServices && m.filterActive
	if activeFilter || m.svcFilter != "" {
		lines = append(lines, padToWidth(filterBar(m.svcFilter, activeFilter), innerW))
	}

	availRows := innerH - len(lines)
	if availRows < 1 {
		availRows = 1
	}

	switch {
	case total == 0:
		lines = append(lines, padToWidth(dimStyle.Render("no services configured"), innerW))
	case len(services) == 0:
		lines = append(lines, padToWidth(dimStyle.Render("no services match filter"), innerW))
	default:
		rows := make([]string, 0, len(services))
		for i, s := range services {
			row := renderServiceRow(i == m.svcCursor && m.focus == paneServices, s, innerW)
			rows = append(rows, padToWidth(clipLine(row, innerW), innerW))
		}
		visible := viewport(rows, m.svcCursor, availRows, &m.svcScroll)
		lines = append(lines, visible...)
	}
	for len(lines) < innerH {
		lines = append(lines, spaces(innerW))
	}

	return style.Render(strings.Join(lines, "\n"))
}

// filterBar renders the single-line filter chrome shown above the list:
// "filter: <text>▌" while typing, "filter: <text>" otherwise. Kept
// unstyled-plain so the caller can pad it reliably to the pane width.
func filterBar(text string, active bool) string {
	label := dimStyle.Render("  filter: ")
	if active {
		return label + text + "▌"
	}
	if text == "" {
		return ""
	}
	return label + text
}

// serviceMetaColWidth is the fixed budget for the trailing meta column
// (site count + pinned/custom tags). Reserved identically on every row so
// the meta column starts at the same column regardless of which tags are
// present, mirroring the aligned layout in the sites pane.
const serviceMetaColWidth = 22

func renderServiceRow(selected bool, s ServiceRow, paneW int) string {
	var glyph string
	switch s.State {
	case stateRunning:
		glyph = runningStyle.Render(glyphRunning)
	case statePaused:
		glyph = pausedStyle.Render(glyphPaused)
	default:
		glyph = stoppedStyle.Render(glyphStopped)
	}

	meta := fmt.Sprintf("(%d site%s)", s.SiteCount, plural(s.SiteCount))
	if s.Pinned {
		meta += "  " + accentStyle.Render("pinned")
	}
	if s.Custom {
		meta += "  " + dimStyle.Render("custom")
	}

	reserved := 5 /* two prefix spaces + glyph + spaces */ + serviceMetaColWidth + 1
	nameW := paneW - reserved
	if nameW < 14 {
		nameW = 14
	}
	name := padRight(truncatePlain(s.Name, nameW), nameW)
	styledName := name
	if selected {
		styledName = selectedStyle.Render(name)
	}

	prefix := " "
	if selected {
		prefix = accentStyle.Render("▸")
	}
	return fmt.Sprintf(" %s %s %s %s", prefix, glyph, styledName, padToWidth(meta, serviceMetaColWidth))
}

func (m *Model) renderLogs(w, h int) string {
	style := unfocusedPane
	innerW, innerH := innerSize(style, w, h)

	target := m.logTail.Target()
	label := target.Label
	if label == "" {
		label = target.ID
	}
	title := fmt.Sprintf("Logs · %s", label)
	if n := len(m.currentLogTargets()); n > 1 {
		title += fmt.Sprintf("   [%d/%d · [ ] to switch]", m.logCursor+1, n)
	}

	availRows := innerH - 1
	if availRows < 1 {
		availRows = 1
	}

	all := m.logTail.Lines()
	total := len(all)

	// Reserve the rightmost column for the scrollbar. Log lines go in
	// contentW; scrollbar gets 1 cell. lipgloss.Width() is skipped here
	// because it treats horizontal padding as part of the width budget,
	// which makes our already-innerW-wide lines wrap to an extra row.
	contentW := innerW - 1
	if contentW < 10 {
		contentW = innerW
	}

	var visible []string
	start := 0
	if total > 0 {
		if total > availRows {
			start = total - availRows
		}
		visible = all[start:]
	}

	body := make([]string, 0, availRows)
	for _, ln := range visible {
		body = append(body, clipLine(ln, contentW))
	}
	if total == 0 {
		body = append(body, clipLine(dimStyle.Render("waiting for output…"), contentW))
	}
	for len(body) < availRows {
		body = append(body, "")
	}

	bar := renderScrollbar(availRows, total, start, len(visible))

	lines := make([]string, 0, availRows+1)
	lines = append(lines, padToWidth(clipLine(sectionStyle.Render(title), innerW), innerW))
	for i := 0; i < availRows; i++ {
		lines = append(lines, padToWidth(body[i], contentW)+bar[i])
	}

	return style.Render(strings.Join(lines, "\n"))
}

// padToWidth right-pads an ANSI-aware string with spaces to `w` display
// cells. Used instead of Go's `%-*s` (which counts bytes) or
// lipgloss.Style.Width (which treats padding as part of the block width
// and causes lines to wrap when we want them to sit flush with the border).
func padToWidth(s string, w int) string {
	n := ansi.StringWidth(s)
	if n >= w {
		return s
	}
	return s + spaces(w-n)
}

// clipLine truncates s to display width w without slicing through an ANSI
// escape or a multi-byte rune. Uses ansi.Truncate so styled log output is
// preserved even when the line is too wide for the pane.
func clipLine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// renderScrollbar returns a slice of height strings (one per content row)
// drawing a vertical scrollbar for a virtual list of `total` items where
// `visible` items starting at `start` are on-screen. Each entry is a
// single-cell string so the caller appends it to the rightmost column.
func renderScrollbar(height, total, start, visible int) []string {
	out := make([]string, height)
	if height <= 0 {
		return out
	}
	if total <= visible || total == 0 {
		for i := range out {
			out[i] = dimStyle.Render("│")
		}
		return out
	}
	thumbSize := height * visible / total
	if thumbSize < 1 {
		thumbSize = 1
	}
	if thumbSize > height {
		thumbSize = height
	}
	// Track position proportionally: thumbStart ∈ [0, height-thumbSize].
	maxStart := total - visible
	thumbStart := 0
	if maxStart > 0 {
		thumbStart = start * (height - thumbSize) / maxStart
	}
	for i := 0; i < height; i++ {
		if i >= thumbStart && i < thumbStart+thumbSize {
			out[i] = accentStyle.Render("█")
		} else {
			out[i] = dimStyle.Render("│")
		}
	}
	return out
}

func paneStyle(focused bool) lipgloss.Style {
	if focused {
		return focusedPane
	}
	return unfocusedPane
}

func innerSize(style lipgloss.Style, w, h int) (int, int) {
	hf := style.GetHorizontalFrameSize()
	vf := style.GetVerticalFrameSize()
	return max(1, w-hf), max(1, h-vf)
}

// viewport returns the slice of rows that fit in `height`, scrolled so the
// cursor stays visible. scroll is updated in place so the pane remembers
// where it was between frames.
func viewport(rows []string, cursor, height int, scroll *int) []string {
	if height <= 0 || len(rows) == 0 {
		return nil
	}
	if cursor < *scroll {
		*scroll = cursor
	}
	if cursor >= *scroll+height {
		*scroll = cursor - height + 1
	}
	if *scroll < 0 {
		*scroll = 0
	}
	end := *scroll + height
	if end > len(rows) {
		end = len(rows)
	}
	return rows[*scroll:end]
}

func truncate(s string, w int) string {
	if w <= 0 || len(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return s[:w-1] + "…"
}

// truncatePlain truncates a rune string by display length without slicing
// through a multi-byte rune. Only safe on unstyled text; never pass ANSI-
// wrapped strings here since escape bytes would count against the budget.
func truncatePlain(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

// padRight right-pads s with spaces to display width w (rune-counted, unstyled).
func padRight(s string, w int) string {
	n := len([]rune(s))
	if n >= w {
		return s
	}
	return s + spaces(w-n)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
