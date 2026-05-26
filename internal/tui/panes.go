package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/geodro/lerd/internal/siteinfo"
)

// narrowWidth is the terminal width below which the TUI switches from the
// two-column layout (sites+services | detail) to a single-column layout
// where only the focused pane fills the full width.
const narrowWidth = 100

// View implements tea.Model.
func (m *Model) View() string {
	if m.width < 60 || m.height < 12 {
		return "terminal too small (need at least 60×12)\n"
	}

	// When a modal is open, return a full-screen centered overlay instead
	// of the base layout. Less ambient context but consistent with the
	// existing detail-pane swap pattern (S / Y / D / F / ?) which already
	// replaces the right column wholesale. Toasts still composite on top
	// so a completing action result isn't silently lost while a modal
	// (palette / confirm / picker / help) is open.
	if m.modalActive() {
		toasts := m.renderToasts(m.width)
		modalH := m.height - lipgloss.Height(toasts)
		if modalH < 6 {
			modalH = m.height
			toasts = ""
		}
		out := m.renderActiveModal(m.width, modalH)
		if toasts != "" {
			out = lipgloss.JoinVertical(lipgloss.Left, out, toasts)
		}
		return out
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	statusBar := m.renderStatus()

	toasts := m.renderToasts(m.width)
	reserved := lipgloss.Height(header) + lipgloss.Height(footer)
	if statusBar != "" {
		reserved += lipgloss.Height(statusBar)
	}
	if toasts != "" {
		reserved += lipgloss.Height(toasts)
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

	var top string
	if m.width < narrowWidth {
		// Narrow: stack list on top, detail below — both always visible.
		// Give the list 40% of the body height, detail gets the rest.
		listH := topH * 2 / 5
		if listH < 6 {
			listH = 6
		}
		if listH > topH-6 {
			listH = topH - 6
		}
		detailH := topH - listH

		switch {
		case m.focus == paneServices:
			// Services focused: take the full height, hide detail.
			top = m.renderServices(m.width, topH)
		case m.detailMode == detailSettings || m.detailMode == detailSystem || m.detailMode == detailDumps || m.detailMode == detailDashboard:
			// Help / settings / system / dumps: take full height so content
			// isn't cramped between the list and a slim detail pane.
			top = m.renderDetailInline(m.width, topH, true)
		default:
			list := m.renderSites(m.width, listH)
			detail := m.renderDetailInline(m.width, detailH, m.focus == paneDetail)
			top = lipgloss.JoinVertical(lipgloss.Left, list, detail)
		}
	} else {
		// Wide: left column stacks sites on top of services; right column is
		// the site detail (full topH). When services is hidden, sites takes
		// the whole left column.
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
			// +3 was the original budget for title + filter + scrollbar
			// padding; the grouped renderer adds up to 2 lines per group
			// header (blank separator + label), so reserve 6 extra cells
			// for the worst case of three visible groups.
			svcNeeded := len(m.snap.Services) + 9
			if svcNeeded < 8 {
				svcNeeded = 8
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
		top = lipgloss.JoinHorizontal(lipgloss.Top, left, detail)
	}

	sections := []string{header, top}
	if m.showLogs {
		sections = append(sections, m.renderLogs(m.width, logH))
	}
	if statusBar != "" {
		sections = append(sections, statusBar)
	}
	if toasts != "" {
		sections = append(sections, toasts)
	}
	sections = append(sections, footer)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *Model) renderHeader() string {
	parts := []string{titleStyle.Render("lerd " + m.version)}

	if m.updateAvailable != "" {
		parts = append(parts, accentStyle.Render("update: "+m.updateAvailable+" (run `lerd update`)"))
	}

	if m.snap.Status.DNSDisabled {
		parts = append(parts, dimStyle.Render("DNS off"))
	} else if m.snap.Status.DNSOk {
		parts = append(parts, runningStyle.Render("DNS ok"))
	} else if m.snap.Status.DNSDegraded {
		parts = append(parts, accentStyle.Render("DNS degraded"))
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

	if names := failingWorkerNames(m.snap); len(names) > 0 {
		parts = append(parts, failingStyle.Render(fmt.Sprintf("⚠ %s failing · H to heal", joinTruncated(names, 3))))
	}

	parts = append(parts, dimStyle.Render(time.Now().Format("15:04:05")))

	return strings.Join(parts, "  ·  ")
}

// countFailingWorkers totals workers in the systemd "failed" state. Kept
// for callers that only need the count (dashboard hero); the header now
// uses failingWorkerNames so users see which units are red, not just how
// many.
func countFailingWorkers(snap Snapshot) int {
	return len(failingWorkerNames(snap))
}

// failingWorkerNames returns kind-site pairs ("queue-acme", "vite-shop")
// for every worker reporting failed across the snapshot. Built-in kinds
// (queue / schedule / horizon / reverb) plus custom framework workers
// plus per-worktree workers all funnel through here so the header pill,
// dashboard hero, and future toast notifier render the same names.
func failingWorkerNames(snap Snapshot) []string {
	var names []string
	add := func(kind, site string, failing bool) {
		if failing {
			names = append(names, kind+"-"+site)
		}
	}
	for _, s := range snap.Sites {
		add("queue", s.Name, s.QueueFailing)
		add("schedule", s.Name, s.ScheduleFailing)
		add("horizon", s.Name, s.HorizonFailing)
		add("reverb", s.Name, s.ReverbFailing)
		for _, fw := range s.FrameworkWorkers {
			add(fw.Name, s.Name, fw.Failing)
		}
		for _, wt := range s.Worktrees {
			for _, fw := range wt.FrameworkWorkers {
				add(fw.Name, s.Name+"/"+wt.Branch, fw.Failing)
			}
		}
	}
	return names
}

// joinTruncated joins names with ", " up to max entries; anything beyond
// is collapsed into "+N more". Keeps the header pill from spilling onto a
// second row when many workers are failing at once.
func joinTruncated(names []string, max int) string {
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:max], ", ") + fmt.Sprintf(" +%d more", len(names)-max)
}

// siteHasFailingWorker is the predicate the sites pane uses to tint a
// row's name with the failing colour. Mirrors the gates in
// failingWorkerNames but scoped to a single site so we avoid the cost of
// rebuilding the global list per row.
func siteHasFailingWorker(s siteinfo.EnrichedSite) bool {
	if s.QueueFailing || s.ScheduleFailing || s.HorizonFailing || s.ReverbFailing {
		return true
	}
	for _, fw := range s.FrameworkWorkers {
		if fw.Failing {
			return true
		}
	}
	for _, wt := range s.Worktrees {
		for _, fw := range wt.FrameworkWorkers {
			if fw.Failing {
				return true
			}
		}
	}
	return false
}

func (m *Model) renderFooter() string {
	if m.filterActive {
		return helpStyle.Render("  filter: type to match · enter apply · esc clear")
	}
	if m.width < narrowWidth {
		keys := []string{
			"tab panes",
			"↑↓ nav",
			"space toggle",
			"l logs",
			"v services",
			"q quit",
		}
		return helpStyle.Render("  " + strings.Join(keys, "   "))
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
		"F dash",
		"S settings",
		"Y system",
		"? help",
		"q quit",
	}
	return helpStyle.Render("  " + strings.Join(keys, "   "))
}

func (m *Model) renderStatus() string {
	// Palette is now a modal overlay (modal.go); status bar only ever
	// renders the most recent action result. An in-flight verb (status
	// ends with "…") gets a spinner glyph so the user sees the action
	// is alive even when the underlying CLI takes seconds to respond.
	if m.status == "" {
		return ""
	}
	if !m.statusExpiry.IsZero() && time.Now().After(m.statusExpiry) {
		m.status = ""
		return ""
	}
	if strings.HasSuffix(strings.TrimSpace(m.status), "…") {
		return "  " + renderSpinnerStatus(m.status)
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

	contentW := innerW - 1
	if contentW < 10 {
		contentW = innerW
	}

	var rowData []string
	switch {
	case total == 0:
		rowData = []string{
			padToWidth(dimStyle.Render("no linked sites yet"), contentW),
			padToWidth("", contentW),
			padToWidth(dimStyle.Render("  cd into a project then run ")+accentStyle.Render("lerd link"), contentW),
			padToWidth(dimStyle.Render("  or open the palette with ")+accentStyle.Render(":")+dimStyle.Render(" and type ")+accentStyle.Render("link"), contentW),
		}
	case len(sites) == 0:
		rowData = []string{
			padToWidth(dimStyle.Render("no sites match filter"), contentW),
			padToWidth(dimStyle.Render("  press ")+accentStyle.Render("esc")+dimStyle.Render(" to clear"), contentW),
		}
	default:
		for i, s := range sites {
			row := renderSiteRow(i == m.siteCursor && m.focus == paneSites, s, contentW)
			rowData = append(rowData, padToWidth(clipLine(row, contentW), contentW))
		}
	}

	visible := viewport(rowData, m.siteCursor, availRows, &m.siteScroll)
	bar := renderScrollbar(availRows, len(rowData), m.siteScroll, len(visible))
	for i := 0; i < availRows; i++ {
		row := ""
		if i < len(visible) {
			row = visible[i]
		}
		lines = append(lines, padToWidth(row, contentW)+bar[i])
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
	case siteHasFailingWorker(s):
		// Tint the whole row so a healthy-looking site with one bad
		// worker isn't mistaken for a fully-green one at a glance.
		// Selected sites keep the accent treatment; failing colour wins
		// only when the user isn't already pointing at this row.
		if selected {
			styled = selectedStyle.Render(name)
		} else {
			styled = failingStyle.Render(name)
		}
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

	contentW := innerW - 1
	if contentW < 10 {
		contentW = innerW
	}

	var rowData []string
	// cursorLine maps svcCursor (an index into the services slice) to the
	// row position in rowData, accounting for non-focusable group headers.
	// Defaults to 0; if grouped rendering inserts headers, this is updated
	// per service-row so viewport keeps the selection on screen.
	cursorLine := 0
	switch {
	case total == 0:
		rowData = []string{
			padToWidth(dimStyle.Render("no services configured"), contentW),
			padToWidth("", contentW),
			padToWidth(dimStyle.Render("  link a site or install a preset (e.g. ")+accentStyle.Render("lerd preset install mysql")+dimStyle.Render(")"), contentW),
		}
	case len(services) == 0:
		rowData = []string{
			padToWidth(dimStyle.Render("no services match filter"), contentW),
			padToWidth(dimStyle.Render("  press ")+accentStyle.Render("esc")+dimStyle.Render(" to clear"), contentW),
		}
	default:
		rowData, cursorLine = renderGroupedServiceRows(services, m.svcCursor, m.focus == paneServices, contentW)
	}

	visible := viewport(rowData, cursorLine, availRows, &m.svcScroll)
	bar := renderScrollbar(availRows, len(rowData), m.svcScroll, len(visible))
	for i := 0; i < availRows; i++ {
		row := ""
		if i < len(visible) {
			row = visible[i]
		}
		lines = append(lines, padToWidth(row, contentW)+bar[i])
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

// serviceGroup labels the bucket a ServiceRow lands in for the grouped
// services pane. Order here drives the visual order: Core first (the
// long-lived presets), then Custom (user-installed), then Workers (the
// per-site fan-out at the bottom because it can be long).
type serviceGroup int

const (
	groupCore serviceGroup = iota
	groupCustom
	groupWorkers
)

func (g serviceGroup) label() string {
	switch g {
	case groupCustom:
		return "Custom"
	case groupWorkers:
		return "Workers"
	}
	return "Core"
}

// classifyService returns the group a row belongs to. Workers carry a
// WorkerKind tag; custom services have Custom=true; everything else is
// a default preset (Core).
func classifyService(s ServiceRow) serviceGroup {
	switch {
	case s.WorkerKind != "":
		return groupWorkers
	case s.Custom:
		return groupCustom
	default:
		return groupCore
	}
}

// renderGroupedServiceRows interleaves dim section headers (Core / Custom
// / Workers) into the service-row stream and reports the line index of
// the focused service so the viewport keeps it visible. Cursor still
// indexes the flat services slice unchanged — only the visual layout is
// grouped, navigation never lands on a header.
func renderGroupedServiceRows(services []ServiceRow, cursor int, paneFocused bool, contentW int) (rows []string, cursorLine int) {
	rows = make([]string, 0, len(services)+3)
	currentGroup := serviceGroup(-1)
	for i, s := range services {
		g := classifyService(s)
		if g != currentGroup {
			if currentGroup != -1 {
				rows = append(rows, padToWidth("", contentW))
			}
			rows = append(rows, padToWidth("  "+sectionStyle.Render(g.label()), contentW))
			currentGroup = g
		}
		if i == cursor && paneFocused {
			cursorLine = len(rows)
		}
		row := renderServiceRow(i == cursor && paneFocused, s, contentW)
		rows = append(rows, padToWidth(clipLine(row, contentW), contentW))
	}
	return rows, cursorLine
}

// serviceMetaColWidth is the fixed budget for the trailing meta column
// (version + site count + pinned/custom tags). Reserved identically on
// every row so the meta column starts at the same column regardless of
// which tags are present, mirroring the aligned layout in the sites pane.
const serviceMetaColWidth = 32

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
	if s.Version != "" {
		meta = dimStyle.Render(s.Version) + "  " + meta
	}
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
	if m.logScroll > 0 {
		title += dimStyle.Render(fmt.Sprintf("   ↑%d  } to tail", m.logScroll))
	}
	if m.logFilter != "" {
		title += "   " + accentStyle.Render("filter: ")
		title += m.logFilter
	}

	availRows := innerH - 1
	if availRows < 1 {
		availRows = 1
	}
	// Filter input bar steals one row when active so the user sees what
	// they're typing without losing the log header.
	if m.logFilterActive {
		availRows--
		if availRows < 1 {
			availRows = 1
		}
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

	// Clamp logScroll so it can't scroll past the beginning.
	if m.logScroll > total-availRows {
		m.logScroll = max(0, total-availRows)
	}

	var visible []string
	start := 0
	if total > 0 {
		end := total - m.logScroll
		if end < availRows {
			end = availRows
		}
		if end > total {
			end = total
		}
		start = end - availRows
		if start < 0 {
			start = 0
		}
		visible = all[start:end]
	}

	body := make([]string, 0, availRows)
	for _, ln := range visible {
		body = append(body, clipLine(styleLogLine(ln, m.logFilter), contentW))
	}
	if total == 0 {
		body = append(body, clipLine(dimStyle.Render("waiting for output…"), contentW))
	}
	for len(body) < availRows {
		body = append(body, "")
	}

	bar := renderScrollbar(availRows, total, start, len(visible))

	lines := make([]string, 0, availRows+2)
	lines = append(lines, padToWidth(clipLine(sectionStyle.Render(title), innerW), innerW))
	if m.logFilterActive {
		lines = append(lines, padToWidth(filterBar(m.logFilter, true), innerW))
	}
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
// where it was between frames. Pass cursor < 0 for pure scroll surfaces
// (no selection); viewport then leaves scroll alone except for clamping
// against the content bounds, so the user's manual scroll position sticks.
func viewport(rows []string, cursor, height int, scroll *int) []string {
	if height <= 0 || len(rows) == 0 {
		return nil
	}
	if cursor >= 0 {
		if cursor < *scroll {
			*scroll = cursor
		}
		if cursor >= *scroll+height {
			*scroll = cursor - height + 1
		}
	}
	if *scroll < 0 {
		*scroll = 0
	}
	if maxScroll := len(rows) - height; *scroll > maxScroll {
		if maxScroll < 0 {
			maxScroll = 0
		}
		*scroll = maxScroll
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
