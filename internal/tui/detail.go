package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/siteinfo"
)

// detailRow is one line in the detail overlay that reacts to input.
// Informational rows use kindInfo and are skipped by cursor navigation.
type detailRow struct {
	kind detailKind
	// Worker kind: logical name used for `lerd queue/schedule/worker start`.
	workerName string
	// Domain kind: the full domain (including the TLD) this row represents.
	domain string
	// Worktree-scoped rows: branch is the sanitized branch name and path is
	// the worktree checkout path; actions cd into path so cwd-keyed CLI
	// helpers (workerNames, FindParentSiteForWorktree) target the right unit.
	branch     string
	branchPath string
}

type detailKind int

const (
	kindInfo detailKind = iota
	kindWorker
	kindHTTPS
	kindLANShare
	kindPHP
	kindNode
	kindDomain
	kindDomainAdd
	kindWorktreeHeader
	kindWorktreeWorker
	kindWorktreeDB
	kindWorktreeLAN
	kindWorktreePHP
	kindWorktreeNode
)

// detailRows returns the ordered rows the detail view draws. Built on each
// render so worker lists stay in sync with live state.
func detailRows(s *siteinfo.EnrichedSite) []detailRow {
	var rows []detailRow
	rows = append(rows, detailRow{kind: kindInfo}) // header placeholder drawn separately
	for _, d := range s.Domains {
		rows = append(rows, detailRow{kind: kindDomain, domain: d})
	}
	rows = append(rows, detailRow{kind: kindDomainAdd})
	if s.HasQueueWorker {
		rows = append(rows, detailRow{kind: kindWorker, workerName: "queue"})
	}
	if s.HasScheduleWorker {
		rows = append(rows, detailRow{kind: kindWorker, workerName: "schedule"})
	}
	if s.HasHorizon {
		rows = append(rows, detailRow{kind: kindWorker, workerName: "horizon"})
	}
	if s.HasReverb {
		rows = append(rows, detailRow{kind: kindWorker, workerName: "reverb"})
	}
	for _, fw := range s.FrameworkWorkers {
		switch fw.Name {
		case "queue", "schedule", "horizon", "reverb":
			continue
		}
		rows = append(rows, detailRow{kind: kindWorker, workerName: fw.Name})
	}
	if s.ContainerPort == 0 && s.PHPVersion != "" {
		rows = append(rows, detailRow{kind: kindPHP})
	}
	if s.NodeVersion != "" {
		rows = append(rows, detailRow{kind: kindNode})
	}
	if cfg, _ := config.LoadGlobal(); cfg == nil || cfg.DNS.Enabled {
		rows = append(rows, detailRow{kind: kindHTTPS})
	}
	rows = append(rows, detailRow{kind: kindLANShare})
	dbCapable := siteHasManagedDB(s)
	for _, wt := range s.Worktrees {
		rows = append(rows, detailRow{kind: kindWorktreeHeader, branch: wt.Branch, branchPath: wt.Path})
		for _, fw := range wt.FrameworkWorkers {
			rows = append(rows, detailRow{
				kind: kindWorktreeWorker, workerName: fw.Name,
				branch: wt.Branch, branchPath: wt.Path,
			})
		}
		if dbCapable {
			rows = append(rows, detailRow{kind: kindWorktreeDB, branch: wt.Branch, branchPath: wt.Path})
		}
		rows = append(rows, detailRow{kind: kindWorktreeLAN, branch: wt.Branch, branchPath: wt.Path})
		if s.ContainerPort == 0 && wt.PHPVersion != "" {
			rows = append(rows, detailRow{kind: kindWorktreePHP, branch: wt.Branch, branchPath: wt.Path})
		}
		if wt.NodeVersion != "" {
			rows = append(rows, detailRow{kind: kindWorktreeNode, branch: wt.Branch, branchPath: wt.Path})
		}
	}
	return rows
}

// siteHasManagedDB reports whether the site uses a lerd-managed database
// service that supports per-worktree isolation. Mirrors the gate the
// dashboard uses to render the Isolated DB toggle.
func siteHasManagedDB(s *siteinfo.EnrichedSite) bool {
	for _, svc := range s.Services {
		switch svc {
		case "mysql", "mariadb", "postgres":
			return true
		}
	}
	return false
}

// findWorktree returns the WorktreeInfo for the given branch, or nil when
// the branch has no live worktree on disk.
func findWorktree(s *siteinfo.EnrichedSite, branch string) *siteinfo.WorktreeInfo {
	for i := range s.Worktrees {
		if s.Worktrees[i].Branch == branch {
			return &s.Worktrees[i]
		}
	}
	return nil
}

// navigableRows filters out info rows so cursor moves skip them.
func navigableRows(rows []detailRow) []int {
	var idx []int
	for i, r := range rows {
		if r.kind != kindInfo {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m *Model) detailToggleSelected(s *siteinfo.EnrichedSite, rows []detailRow, nav []int) tea.Cmd {
	if s == nil || len(nav) == 0 {
		return nil
	}
	if m.detailCursor >= len(nav) {
		m.detailCursor = len(nav) - 1
	}
	row := rows[nav[m.detailCursor]]
	switch row.kind {
	case kindWorker:
		return m.toggleWorker(s, row.workerName)
	case kindHTTPS:
		if s.Secured {
			m.setStatus("disabling HTTPS for "+s.Name+"…", 5*time.Second)
			return runLerd(s.Path, "unsecure", s.Name)
		}
		m.setStatus("enabling HTTPS for "+s.Name+"…", 5*time.Second)
		return runLerd(s.Path, "secure", s.Name)
	case kindLANShare:
		if s.LANPort > 0 {
			m.setStatus("stopping LAN share for "+s.Name+"…", 5*time.Second)
			return runLerd(s.Path, "lan", "unshare")
		}
		m.setStatus("starting LAN share for "+s.Name+"…", 5*time.Second)
		return runLerd(s.Path, "lan", "share")
	case kindPHP:
		m.openPHPPicker(s)
		return nil
	case kindNode:
		m.openNodePicker(s)
		return nil
	case kindDomain:
		// Selecting a domain does nothing on its own; removal is `x`.
		return nil
	case kindDomainAdd:
		m.openDomainInput()
		return nil
	case kindWorktreeWorker:
		return m.toggleWorktreeWorker(s, row)
	case kindWorktreeDB:
		return m.toggleWorktreeDB(s, row)
	case kindWorktreeLAN:
		return m.toggleWorktreeLAN(s, row)
	case kindWorktreePHP:
		m.openWorktreePHPPicker(s, row)
		return nil
	case kindWorktreeNode:
		m.openWorktreeNodePicker(s, row)
		return nil
	}
	return nil
}

func (m *Model) toggleWorktreeLAN(s *siteinfo.EnrichedSite, row detailRow) tea.Cmd {
	wt := findWorktree(s, row.branch)
	if wt == nil {
		return nil
	}
	if wt.LANPort > 0 {
		m.setStatus("stopping LAN share on "+row.branch+"…", 5*time.Second)
		return runLerd(row.branchPath, "lan", "unshare")
	}
	m.setStatus("starting LAN share on "+row.branch+"…", 5*time.Second)
	return runLerd(row.branchPath, "lan", "share")
}

func (m *Model) toggleWorktreeWorker(s *siteinfo.EnrichedSite, row detailRow) tea.Cmd {
	wt := findWorktree(s, row.branch)
	if wt == nil {
		return nil
	}
	running := worktreeWorkerRunning(wt, row.workerName)
	verb := "start"
	if running {
		verb = "stop"
	}
	m.setStatus(verb+"ing "+row.workerName+" on "+row.branch+"…", 5*time.Second)
	return runLerd(row.branchPath, "worker", verb, row.workerName)
}

func (m *Model) toggleWorktreeDB(s *siteinfo.EnrichedSite, row detailRow) tea.Cmd {
	wt := findWorktree(s, row.branch)
	if wt == nil {
		return nil
	}
	if wt.DBIsolated {
		m.setStatus("sharing parent DB on "+row.branch+"…", 5*time.Second)
		return runLerd(row.branchPath, "db:share")
	}
	m.setStatus("isolating DB on "+row.branch+"…", 5*time.Second)
	return runLerd(row.branchPath, "db:isolate")
}

func worktreeWorkerRunning(wt *siteinfo.WorktreeInfo, name string) bool {
	if wt == nil {
		return false
	}
	for _, fw := range wt.FrameworkWorkers {
		if fw.Name == name {
			return fw.Running
		}
	}
	return false
}

func worktreeWorkerFailing(wt *siteinfo.WorktreeInfo, name string) bool {
	if wt == nil {
		return false
	}
	for _, fw := range wt.FrameworkWorkers {
		if fw.Name == name {
			return fw.Failing
		}
	}
	return false
}

func worktreeWorkerLabel(wt *siteinfo.WorktreeInfo, name string) string {
	if wt == nil {
		return name
	}
	for _, fw := range wt.FrameworkWorkers {
		if fw.Name == name && fw.Label != "" {
			return fw.Label
		}
	}
	return name
}

// removeFocusedDomain runs `lerd domain remove <name>` for the domain the
// cursor is on. Does nothing when focus is elsewhere or a different kind of
// row is selected — meaning `x` on workers or services still does stop.
func (m *Model) removeFocusedDomain() (handled bool, cmd tea.Cmd) {
	if m.focus != paneDetail || m.detailMode != detailSite {
		return false, nil
	}
	s := m.currentSite()
	if s == nil {
		return false, nil
	}
	rows := detailRows(s)
	nav := navigableRows(rows)
	if m.detailCursor >= len(nav) {
		return false, nil
	}
	row := rows[nav[m.detailCursor]]
	if row.kind != kindDomain {
		return false, nil
	}
	short := trimTLD(row.domain)
	m.setStatus("removing domain "+row.domain+"…", 5*time.Second)
	return true, runLerd(s.Path, "domain", "remove", short)
}

// trimTLD strips the configured TLD suffix from a full domain so the short
// form is what `lerd domain add/remove` expects. Falls back to stripping
// the last dotted component if the config can't be read.
func trimTLD(full string) string {
	cfg, _ := config.LoadGlobal()
	if cfg != nil && cfg.DNS.TLD != "" {
		if trimmed := strings.TrimSuffix(full, "."+cfg.DNS.TLD); trimmed != full {
			return trimmed
		}
	}
	if i := strings.LastIndexByte(full, '.'); i >= 0 {
		return full[:i]
	}
	return full
}

// currentTLD returns the configured TLD, defaulting to "test" when global
// config can't be read. Centralised so the handful of call sites don't
// each have to inline the fallback.
func currentTLD() string {
	cfg, _ := config.LoadGlobal()
	if cfg != nil && cfg.DNS.TLD != "" {
		return cfg.DNS.TLD
	}
	return "test"
}

func (m *Model) toggleWorker(s *siteinfo.EnrichedSite, name string) tea.Cmd {
	running := workerRunning(s, name)
	verb := "start"
	if running {
		verb = "stop"
	}
	m.setStatus(verb+"ing "+name+" worker for "+s.Name+"…", 5*time.Second)
	switch name {
	case "queue":
		return runLerd(s.Path, "queue", verb)
	case "schedule":
		return runLerd(s.Path, "schedule", verb)
	case "horizon":
		return runLerd(s.Path, "horizon", verb)
	case "reverb":
		return runLerd(s.Path, "reverb", verb)
	default:
		return runLerd(s.Path, "worker", verb, name)
	}
}

func workerRunning(s *siteinfo.EnrichedSite, name string) bool {
	switch name {
	case "queue":
		return s.QueueRunning
	case "schedule":
		return s.ScheduleRunning
	case "horizon":
		return s.HorizonRunning
	case "reverb":
		return s.ReverbRunning
	}
	for _, fw := range s.FrameworkWorkers {
		if fw.Name == name {
			return fw.Running
		}
	}
	return false
}

func workerFailing(s *siteinfo.EnrichedSite, name string) bool {
	switch name {
	case "queue":
		return s.QueueFailing
	case "schedule":
		return s.ScheduleFailing
	case "horizon":
		return s.HorizonFailing
	case "reverb":
		return s.ReverbFailing
	}
	for _, fw := range s.FrameworkWorkers {
		if fw.Name == name {
			return fw.Failing
		}
	}
	return false
}

func workerLabel(s *siteinfo.EnrichedSite, name string) string {
	for _, fw := range s.FrameworkWorkers {
		if fw.Name == name && fw.Label != "" {
			return fw.Label
		}
	}
	return name
}

// renderDetailInline builds the right-column pane: full-height site detail
// by default, or the global settings rows when detailMode == detailSettings.
// Both live in the same pane so `S` is a toggle, not a separate screen.
func (m *Model) renderDetailInline(w, h int, focused bool) string {
	style := paneStyle(focused)
	innerW, innerH := innerSize(style, w, h)

	contentW := innerW - 1 // reserve 1 cell for scrollbar

	var content []string
	cursorLine := 0
	switch m.detailMode {
	case detailSettings:
		content = settingsContentLines(m, focused, contentW)
	case detailHelp:
		all := helpContentLines(m, contentW)
		if m.helpScroll > len(all)-1 {
			m.helpScroll = max(0, len(all)-1)
		}
		content = all[m.helpScroll:]
	case detailDumps:
		content, cursorLine = dumpsContentLines(m, focused, contentW)
	default:
		site := m.currentSite()
		if site == nil {
			content = []string{
				padToWidth(sectionStyle.Render("Site detail"), contentW),
				padToWidth(dimStyle.Render("no site selected"), contentW),
			}
		} else {
			content, cursorLine = detailContentLines(m, site, focused, contentW)
		}
	}

	visible := viewport(content, cursorLine, innerH, &m.detailScroll)
	bar := renderScrollbar(innerH, len(content), m.detailScroll, len(visible))

	lines := make([]string, 0, innerH)
	for i := 0; i < innerH; i++ {
		row := spaces(contentW)
		if i < len(visible) {
			row = visible[i]
		}
		lines = append(lines, padToWidth(row, contentW)+bar[i])
	}

	return style.Render(strings.Join(lines, "\n"))
}

// settingsContentLines builds the settings rows for the right-hand pane when
// detailMode == detailSettings. Mirrors the detail pane's look so S feels
// like a pane swap, not a modal.
func settingsContentLines(m *Model, focused bool, innerW int) []string {
	rows := m.settingsRows()
	out := make([]string, 0, len(rows)+4)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	add(sectionStyle.Render("Settings"))
	add(dimStyle.Render("  press S again to return to site detail"))
	add("")

	if len(rows) == 0 {
		add(dimStyle.Render("  no settings available"))
		return out
	}

	for i, row := range rows {
		selected := focused && i == m.settingsRow
		add(renderDetailRow(selected, onOffGlyph(row.on), row.label, onOffText(row.on)))
	}
	return out
}

// detailContentLines returns the rendered lines for the site detail pane and
// the line index of the currently selected row (for viewport scrolling).
func detailContentLines(m *Model, site *siteinfo.EnrichedSite, focused bool, innerW int) ([]string, int) {
	rows := detailRows(site)
	nav := navigableRows(rows)
	navPos := func(i int) int {
		for pos, rowIdx := range nav {
			if rowIdx == i {
				return pos
			}
		}
		return -1
	}

	var out []string
	cursorLine := 0
	add := func(s string, selected bool) {
		if selected && len(out) > 0 {
			cursorLine = len(out)
		}
		out = append(out, padToWidth(clipLine(s, innerW), innerW))
	}
	addPlain := func(s string) { add(s, false) }

	// Lead with the primary domain (what users see in the browser). The
	// internal registry name is still surfaced as the "name:" line below,
	// since commands and filters still accept it.
	header := site.PrimaryDomain()
	if header == "" {
		header = site.Name
	}
	addPlain(sectionStyle.Render(header))
	if site.Name != header {
		addPlain(dimStyle.Render("  name: ") + site.Name)
	}
	if site.Path != "" {
		addPlain(dimStyle.Render("  path: ") + site.Path)
	}

	scheme := "http"
	if site.Secured {
		scheme = "https"
	}

	addPlain("")
	addPlain(sectionStyle.Render("Domains"))
	if len(site.Domains) == 0 {
		addPlain(dimStyle.Render("  (no domain)"))
	}
	for i, row := range rows {
		if row.kind != kindDomain {
			continue
		}
		selected := focused && navPos(i) == m.detailCursor
		domain := row.domain
		label := scheme + "://" + domain
		add(renderDetailRow(selected, accentStyle.Render("⊙"), label, dimStyle.Render(domainRole(site, domain))), selected)
	}
	for i, row := range rows {
		if row.kind != kindDomainAdd {
			continue
		}
		selected := focused && navPos(i) == m.detailCursor
		prefix := "  "
		if selected {
			prefix = " " + accentStyle.Render("▸")
		}
		if m.domainInputActive {
			label := "add domain: "
			if m.domainInputEditing != "" {
				label = "rename " + m.domainInputEditing + " → "
			}
			add(prefix+" "+accentStyle.Render("+")+" "+selectedStyle.Render(label)+m.domainInput+"▌", selected)
		} else {
			add(prefix+" "+accentStyle.Render("+")+" "+dimStyle.Render("add domain (space or a)"), selected)
		}
	}
	addPlain("")

	php := site.PHPVersion
	if php == "" && site.ContainerPort > 0 {
		php = "custom"
	}
	info := dimStyle.Render("  php: ") + php
	if site.NodeVersion != "" {
		info += dimStyle.Render("  node: ") + site.NodeVersion
	}
	if site.FrameworkLabel != "" {
		info += dimStyle.Render("  fw: ") + site.FrameworkLabel
	}
	if site.Runtime == "frankenphp" {
		rt := "frankenphp"
		if site.RuntimeWorker {
			rt = "frankenphp (worker)"
		}
		info += dimStyle.Render("  runtime: ") + accentStyle.Render(rt)
	}
	if site.Branch != "" {
		info += dimStyle.Render("  git: ") + site.Branch
	}
	addPlain(info)
	addPlain("")
	if len(site.Services) > 0 {
		addPlain(sectionStyle.Render("Services used"))
		states := m.serviceStatesByName()
		for _, svc := range site.Services {
			addPlain(renderSiteServiceRow(svc, states[svc]))
		}
		addPlain("")
	}

	hasWorkers := false
	for _, row := range rows {
		if row.kind == kindWorker {
			hasWorkers = true
			break
		}
	}
	if hasWorkers {
		addPlain(sectionStyle.Render("Workers"))
		for i, row := range rows {
			if row.kind != kindWorker {
				continue
			}
			selected := focused && navPos(i) == m.detailCursor
			add(renderDetailRow(selected,
				workerGlyphFor(site, row.workerName),
				workerLabel(site, row.workerName),
				workerStateText(site, row.workerName)), selected)
		}
		addPlain("")
	}

	if len(site.Worktrees) > 0 {
		addPlain(sectionStyle.Render("Worktrees"))
		for _, wt := range site.Worktrees {
			head := "  " + accentStyle.Render(wt.Branch)
			if wt.Domain != "" {
				head += "  " + dimStyle.Render(scheme+"://"+wt.Domain)
			}
			if wt.Path != "" {
				head += "  " + dimStyle.Render(wt.Path)
			}
			addPlain(head)
			renderedAny := false
			for i, row := range rows {
				if row.kind == kindWorktreeWorker && row.branch == wt.Branch {
					renderedAny = true
					selected := focused && navPos(i) == m.detailCursor
					add(renderDetailRow(selected,
						worktreeWorkerGlyph(&wt, row.workerName),
						"    "+worktreeWorkerLabel(&wt, row.workerName),
						worktreeWorkerStateText(&wt, row.workerName)), selected)
				}
			}
			for i, row := range rows {
				if row.kind == kindWorktreeDB && row.branch == wt.Branch {
					renderedAny = true
					selected := focused && navPos(i) == m.detailCursor
					add(renderDetailRow(selected,
						onOffGlyph(wt.DBIsolated),
						"    "+"Isolated DB",
						worktreeDBStateText(wt)), selected)
				}
			}
			for i, row := range rows {
				if row.kind == kindWorktreeLAN && row.branch == wt.Branch {
					renderedAny = true
					selected := focused && navPos(i) == m.detailCursor
					add(renderDetailRow(selected,
						onOffGlyph(wt.LANPort > 0),
						"    "+"LAN share",
						lanShareText(wt.LANPort)), selected)
				}
			}
			for i, row := range rows {
				if row.kind == kindWorktreePHP && row.branch == wt.Branch {
					renderedAny = true
					selected := focused && navPos(i) == m.detailCursor
					add(renderDetailRow(selected,
						accentStyle.Render("λ"),
						"    "+"PHP",
						worktreeVersionText(wt.PHPVersion, wt.PHPVersionOverride)), selected)
					if selected && m.pickerKind == kindWorktreePHP && m.pickerWorktreeName == wt.Branch {
						for _, pl := range m.renderPickerRows(wt.PHPVersion) {
							addPlain(pl)
						}
					}
				}
				if row.kind == kindWorktreeNode && row.branch == wt.Branch {
					renderedAny = true
					selected := focused && navPos(i) == m.detailCursor
					add(renderDetailRow(selected,
						accentStyle.Render("⬢"),
						"    "+"Node",
						worktreeVersionText(wt.NodeVersion, wt.NodeVersionOverride)), selected)
					if selected && m.pickerKind == kindWorktreeNode && m.pickerWorktreeName == wt.Branch {
						for _, pl := range m.renderPickerRows(wt.NodeVersion) {
							addPlain(pl)
						}
					}
				}
			}
			if !renderedAny {
				addPlain(dimStyle.Render("    (no per-worktree controls)"))
			}
		}
		addPlain("")
	}

	addPlain(sectionStyle.Render("Toggles"))
	for i, row := range rows {
		selected := focused && navPos(i) == m.detailCursor
		switch row.kind {
		case kindPHP:
			add(renderDetailRow(selected,
				accentStyle.Render("λ"), "PHP", dimStyle.Render(site.PHPVersion)), selected)
			if selected && m.pickerKind == kindPHP {
				for _, pl := range m.renderPickerRows(site.PHPVersion) {
					addPlain(pl)
				}
			}
		case kindNode:
			add(renderDetailRow(selected,
				accentStyle.Render("⬢"), "Node", dimStyle.Render(site.NodeVersion)), selected)
			if selected && m.pickerKind == kindNode {
				for _, pl := range m.renderPickerRows(site.NodeVersion) {
					addPlain(pl)
				}
			}
		case kindHTTPS:
			add(renderDetailRow(selected,
				onOffGlyph(site.Secured), "HTTPS", onOffText(site.Secured)), selected)
		case kindLANShare:
			add(renderDetailRow(selected,
				onOffGlyph(site.LANPort > 0), "LAN share", lanShareText(site.LANPort)), selected)
		}
	}
	return out, cursorLine
}

// renderPickerRows draws the inline version picker below a PHP or Node row.
// Each option shows a cursor, the version, and a dim "current" marker when
// it matches the site's active version.
func (m *Model) renderPickerRows(current string) []string {
	if len(m.pickerOptions) == 0 {
		return []string{dimStyle.Render("      (no versions available)")}
	}
	out := make([]string, 0, len(m.pickerOptions)+1)
	for i, v := range m.pickerOptions {
		marker := "  "
		if i == m.pickerCursor {
			marker = accentStyle.Render("▸ ")
		}
		label := v
		if i == m.pickerCursor {
			label = selectedStyle.Render(v)
		}
		suffix := ""
		if v == current {
			suffix = "  " + dimStyle.Render("(current)")
		}
		out = append(out, "      "+marker+label+suffix)
	}
	out = append(out, dimStyle.Render("      enter apply · esc cancel"))
	return out
}

func renderDetailRow(selected bool, glyph, label, state string) string {
	prefix := "  "
	if selected {
		prefix = " " + accentStyle.Render("▸")
	}
	// Pad short labels to a minimum of 18 cells so state columns across
	// rows line up, but do NOT truncate long labels — long values like a
	// full https URL need the whole pane width. The outer clipLine call
	// handles final truncation at innerW, so overflow is bounded.
	padded := label
	if w := len([]rune(label)); w < 18 {
		padded = label + spaces(18-w)
	}
	if selected {
		padded = selectedStyle.Render(padded)
	}
	return fmt.Sprintf("%s %s %s %s", prefix, glyph, padded, state)
}

// serviceStatesByName maps service name → live state from the current
// snapshot. Used by the detail pane's "Services used" section so each
// service the site references shows its actual running/stopped/paused
// state instead of the raw name list.
func (m *Model) serviceStatesByName() map[string]ServiceState {
	out := make(map[string]ServiceState, len(m.snap.Services))
	for _, s := range m.snap.Services {
		out[s.Name] = s.State
	}
	return out
}

// renderSiteServiceRow draws one row in the detail pane's "Services used"
// section: glyph, service name, and live state text. Missing entries (a
// service referenced by the site but not present in the snapshot, e.g. a
// removed custom service) render as dim "not configured".
func renderSiteServiceRow(name string, state ServiceState) string {
	var glyph, text string
	switch state {
	case stateRunning:
		glyph = runningStyle.Render(glyphRunning)
		text = runningStyle.Render("running")
	case statePaused:
		glyph = pausedStyle.Render(glyphPaused)
		text = pausedStyle.Render("paused")
	default:
		glyph = stoppedStyle.Render(glyphStopped)
		text = dimStyle.Render("stopped")
	}
	padded := name
	if w := len([]rune(name)); w < 18 {
		padded = name + spaces(18-w)
	}
	return "  " + glyph + " " + padded + " " + text
}

// domainRole labels a domain's position in the list. Exactly one domain is
// the primary (the first in site.Domains); the others are aliases. The web
// UI renders the same distinction, so the TUI shouldn't invent new terms.
func domainRole(s *siteinfo.EnrichedSite, domain string) string {
	role := "alias"
	if len(s.Domains) > 0 && s.Domains[0] == domain {
		role = "primary"
	}
	return role + " · e edit · x remove"
}

func worktreeWorkerGlyph(wt *siteinfo.WorktreeInfo, name string) string {
	switch {
	case worktreeWorkerFailing(wt, name):
		return failingStyle.Render(glyphFailing)
	case worktreeWorkerRunning(wt, name):
		return runningStyle.Render(glyphRunning)
	}
	return stoppedStyle.Render(glyphStopped)
}

func worktreeWorkerStateText(wt *siteinfo.WorktreeInfo, name string) string {
	switch {
	case worktreeWorkerFailing(wt, name):
		return failingStyle.Render("failing")
	case worktreeWorkerRunning(wt, name):
		return runningStyle.Render("running")
	}
	return dimStyle.Render("stopped")
}

func worktreeDBStateText(wt siteinfo.WorktreeInfo) string {
	if wt.DBIsolated {
		name := wt.DBDatabase
		if name == "" {
			name = "isolated"
		}
		return runningStyle.Render(name)
	}
	return dimStyle.Render("shared with parent")
}

// worktreeVersionText shows the effective PHP/Node version with an
// "(inherited)" hint when the value comes from the parent rather than a
// .lerd.yaml override on the worktree.
func worktreeVersionText(version string, override bool) string {
	if version == "" {
		return dimStyle.Render("not set")
	}
	if override {
		return accentStyle.Render(version)
	}
	return dimStyle.Render(version + " (inherited)")
}

func workerGlyphFor(s *siteinfo.EnrichedSite, name string) string {
	switch {
	case workerFailing(s, name):
		return failingStyle.Render(glyphFailing)
	case workerRunning(s, name):
		return runningStyle.Render(glyphRunning)
	}
	return stoppedStyle.Render(glyphStopped)
}

func workerStateText(s *siteinfo.EnrichedSite, name string) string {
	switch {
	case workerFailing(s, name):
		return failingStyle.Render("failing")
	case workerRunning(s, name):
		return runningStyle.Render("running")
	}
	return dimStyle.Render("stopped")
}

func onOffGlyph(on bool) string {
	if on {
		return runningStyle.Render(glyphRunning)
	}
	return stoppedStyle.Render(glyphStopped)
}

func onOffText(on bool) string {
	if on {
		return runningStyle.Render("on")
	}
	return dimStyle.Render("off")
}

func lanShareText(port int) string {
	if port <= 0 {
		return dimStyle.Render("off")
	}
	ip := primaryLANIP()
	if ip == "" {
		return runningStyle.Render(fmt.Sprintf("sharing on port %d", port))
	}
	return runningStyle.Render(fmt.Sprintf("http://%s:%d", ip, port))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
