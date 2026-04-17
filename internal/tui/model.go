package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/eventbus"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteinfo"
	lerdUpdate "github.com/geodro/lerd/internal/update"
)

// focusPane identifies which pane currently owns keyboard focus. Detail sits
// permanently in the right column below the services list, so it's always
// reachable via tab cycling.
type focusPane int

const (
	paneSites focusPane = iota
	paneServices
	paneDetail
)

// detailMode picks what the right-hand pane renders. Site detail is the
// default; pressing S swaps it for settings and ? swaps it for the full
// keybinding reference. Kept as a single pane, not a separate overlay,
// so the user never leaves the main dashboard.
type detailMode int

const (
	detailSite detailMode = iota
	detailSettings
	detailHelp
)

// Model is the bubbletea root. Panes are all projections of snap plus small
// per-pane cursor/scroll state, so every refresh cycle rebuilds from a single
// source of truth without stale data.
type Model struct {
	width, height int

	snap Snapshot

	detailMode detailMode
	focus      focusPane

	siteCursor int
	siteScroll int
	siteFilter string
	siteSort   siteSortMode

	svcCursor int
	svcScroll int
	svcFilter string
	svcSort   svcSortMode

	// Filter-input mode: when active, key runes go into the active filter
	// instead of firing actions. Which pane is being filtered is determined
	// by focus. Exited by esc or enter.
	filterActive bool

	detailCursor int // index into detail rows (workers + toggles)
	settingsRow  int // index into settings rows
	helpScroll   int // vertical scroll offset for the help view

	// Picker state (PHP/Node version). When active, up/down navigates
	// pickerOptions instead of detail rows and enter applies the pick.
	pickerKind    detailKind
	pickerOptions []string
	pickerCursor  int

	// Domain-input state: when active, typing adds characters to the
	// pending domain name; enter runs `lerd domain add`, esc cancels.
	// When domainInputEditing is non-empty, the input is editing that
	// existing full domain — commit chains an add-new + remove-old.
	domainInputActive  bool
	domainInput        string
	domainInputEditing string

	showLogs     bool
	hideServices bool
	logTail      *logTail
	logCursor    int // index into currentLogTargets() for the focused item

	status       string
	statusExpiry time.Time

	sub     *eventbus.Subscriber
	version string

	// Latest version reported by the 24h-cached update check. Non-empty
	// when a newer release is available; rendered as a banner in the
	// header so users see it without running lerd status.
	updateAvailable string
}

// NewModel builds an initial model. The caller is expected to call
// podman.Cache.Start before running; NewModel itself is pure.
func NewModel(version string) *Model {
	return &Model{
		width:   100,
		height:  30,
		logTail: newLogTail(),
		sub:     eventbus.Default.Subscribe(),
		version: version,
	}
}

// Init implements tea.Model. Kicks off the first snapshot load, the refresh
// ticker, the eventbus subscription, and a one-shot update check.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		loadCmd(),
		tickCmd(2*time.Second),
		busCmd(m.sub),
		updateCheckCmd(m.version),
	)
}

// updateCheckMsg carries the result of the cached update lookup back into
// the model. Empty Latest means "on the latest version or check failed".
type updateCheckMsg struct{ Latest string }

// updateCheckCmd reads the 24h update cache (populated by lerd status /
// lerd doctor). We don't fire a live GitHub request ourselves — if no
// other command has run lately, the banner just stays hidden.
func updateCheckCmd(current string) tea.Cmd {
	return func() tea.Msg {
		info, _ := lerdUpdate.CachedUpdateCheck(current)
		if info == nil {
			return updateCheckMsg{}
		}
		return updateCheckMsg{Latest: info.LatestVersion}
	}
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case refreshMsg:
		return m, loadCmd()

	case snapshotMsg:
		m.snap = msg.snap
		m.clampCursors()
		return m, tickCmd(2 * time.Second)

	case ActionResult:
		m.setStatus(formatAction(msg), 5*time.Second)
		return m, loadCmd()

	case logLineMsg:
		if msg.source == m.logTail.Source() {
			return m, m.logTail.Follow()
		}
		return m, nil

	case logClosedMsg:
		return m, nil

	case updateCheckMsg:
		m.updateAvailable = msg.Latest
		return m, nil
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.handleMainKey(msg)
}

func (m *Model) handleMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filterActive {
		return m.handleFilterKey(msg)
	}
	if m.domainInputActive {
		return m.handleDomainInputKey(msg)
	}
	switch msg.String() {
	case "ctrl+c", "q":
		m.logTail.Stop()
		eventbus.Default.Unsubscribe(m.sub)
		return m, tea.Quit

	case "enter", " ":
		if m.focus == paneDetail {
			if m.pickerKind != kindInfo {
				return m, m.applyPicker()
			}
			if m.detailMode == detailSettings {
				return m, m.settingsToggle(m.settingsRows())
			}
			return m, m.detailToggleSelected(m.currentSite(), detailRows(m.currentSite()), navigableRows(detailRows(m.currentSite())))
		}
		return m, nil

	case "esc":
		if m.pickerKind != kindInfo {
			m.closePicker()
			return m, nil
		}
		if m.detailMode != detailSite {
			m.detailMode = detailSite
		}
		return m, nil

	case "S":
		if m.detailMode == detailSettings {
			m.detailMode = detailSite
		} else {
			m.detailMode = detailSettings
			m.settingsRow = 0
			// Jump focus into the detail pane so the user can move through
			// settings immediately without a separate tab press. No-op when
			// already focused there.
			m.focus = paneDetail
		}
		return m, nil

	case "?":
		if m.detailMode == detailHelp {
			m.detailMode = detailSite
		} else {
			m.detailMode = detailHelp
			m.focus = paneDetail
		}
		return m, nil

	case "tab":
		m.focus = m.nextFocus(+1)
		return m, m.syncLogs()

	case "shift+tab":
		m.focus = m.nextFocus(-1)
		return m, m.syncLogs()

	case "up", "k":
		m.moveCursor(-1)
		return m, m.syncLogs()

	case "down", "j":
		m.moveCursor(1)
		return m, m.syncLogs()

	case "pgup":
		m.moveCursor(-10)
		return m, m.syncLogs()

	case "pgdown":
		m.moveCursor(10)
		return m, m.syncLogs()

	case "home", "g":
		m.setCursor(0)
		return m, m.syncLogs()

	case "end", "G":
		m.setCursor(1 << 30)
		return m, m.syncLogs()

	case "l":
		return m, m.toggleLogs()

	case "t":
		return m, m.actionShell()

	case "v":
		m.hideServices = !m.hideServices
		if m.hideServices && m.focus == paneServices {
			m.focus = paneSites
		}
		return m, nil

	case "/":
		if m.focus == paneSites || m.focus == paneServices {
			m.filterActive = true
		}
		return m, nil

	case "o":
		switch m.focus {
		case paneSites:
			m.siteSort = (m.siteSort + 1) % 3
			m.siteCursor = 0
		case paneServices:
			m.svcSort = (m.svcSort + 1) % 3
			m.svcCursor = 0
		}
		return m, nil

	case "[":
		return m, m.cycleLogTarget(-1)

	case "]":
		return m, m.cycleLogTarget(1)

	case "s":
		return m, m.actionStart()

	case "x":
		if handled, cmd := m.removeFocusedDomain(); handled {
			return m, cmd
		}
		return m, m.actionStop()

	case "a":
		if m.focus == paneDetail && m.detailMode == detailSite && m.currentSite() != nil {
			m.openDomainInput()
			return m, nil
		}
		return m, nil

	case "e":
		if m.editFocusedDomain() {
			return m, nil
		}
		return m, nil

	case "r":
		return m, m.actionRestart()

	case "p":
		return m, m.actionPauseToggle()

	case "R":
		return m, loadCmd()
	}
	return m, nil
}

// openDomainInput switches into domain-input mode for adding a new domain.
// Focus is pinned to paneDetail so the user sees the input next to the
// domains list they were just looking at.
func (m *Model) openDomainInput() {
	m.domainInputActive = true
	m.domainInput = ""
	m.domainInputEditing = ""
	m.focus = paneDetail
}

// openDomainEdit enters domain-input mode pre-filled with the short form
// of `full`. On commit the handler runs add-new + remove-old as a sequence,
// which gives rename semantics without a dedicated `lerd domain rename`
// command.
func (m *Model) openDomainEdit(full string) {
	m.domainInputActive = true
	m.domainInput = trimTLD(full)
	m.domainInputEditing = full
	m.focus = paneDetail
}

// editFocusedDomain is called from handleMainKey when `e` is pressed with
// the detail cursor on a domain row. Returns false when the focus/row
// doesn't match so other bindings keep working.
func (m *Model) editFocusedDomain() (handled bool) {
	if m.focus != paneDetail || m.detailMode != detailSite {
		return false
	}
	s := m.currentSite()
	if s == nil {
		return false
	}
	rows := detailRows(s)
	nav := navigableRows(rows)
	if m.detailCursor >= len(nav) {
		return false
	}
	row := rows[nav[m.detailCursor]]
	if row.kind != kindDomain {
		return false
	}
	m.openDomainEdit(row.domain)
	return true
}

// handleDomainInputKey collects characters for the new domain, commits on
// enter (running `lerd domain add <short>` from the site dir), cancels on
// esc. Unlike filter input we're not narrowing a list in real time, so no
// refresh is needed until the subprocess exits.
func (m *Model) handleDomainInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.domainInputActive = false
		m.domainInput = ""
		m.domainInputEditing = ""
		return m, nil
	case "enter":
		s := m.currentSite()
		value := strings.TrimSpace(m.domainInput)
		editing := m.domainInputEditing
		m.domainInputActive = false
		m.domainInput = ""
		m.domainInputEditing = ""
		if s == nil || value == "" {
			return m, nil
		}
		value = strings.ToLower(strings.TrimSuffix(value, "."+currentTLD()))
		if editing != "" {
			// No-op if the user pressed enter without changing anything.
			if value == trimTLD(editing) {
				return m, nil
			}
			oldShort := trimTLD(editing)
			m.setStatus("renaming "+editing+" → "+value+"…", 5*time.Second)
			return m, tea.Sequence(
				runLerd(s.Path, "domain", "add", value),
				runLerd(s.Path, "domain", "remove", oldShort),
			)
		}
		m.setStatus("adding domain "+value+"…", 5*time.Second)
		return m, runLerd(s.Path, "domain", "add", value)
	case "ctrl+c":
		m.logTail.Stop()
		return m, tea.Quit
	case "backspace":
		if len(m.domainInput) > 0 {
			r := []rune(m.domainInput)
			m.domainInput = string(r[:len(r)-1])
		}
	default:
		if len(msg.Runes) > 0 {
			m.domainInput += string(msg.Runes)
		}
	}
	return m, nil
}

// handleFilterKey runs while the filter input is active. Typed runes are
// appended to the filter for the currently focused pane; backspace removes
// the last rune; enter and esc exit input mode (esc also clears the
// filter). Actions, tab, navigation are all suppressed while filter mode
// is active so the user can type freely.
func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	target := m.filterTarget()
	switch msg.String() {
	case "esc":
		*target = ""
		m.filterActive = false
		m.resetFilteredCursor()
	case "enter":
		m.filterActive = false
	case "ctrl+c":
		m.logTail.Stop()
		return m, tea.Quit
	case "backspace":
		if len(*target) > 0 {
			r := []rune(*target)
			*target = string(r[:len(r)-1])
			m.resetFilteredCursor()
		}
	default:
		if len(msg.Runes) > 0 {
			*target += string(msg.Runes)
			m.resetFilteredCursor()
		}
	}
	return m, nil
}

// filterTarget returns the pointer to the filter string for whichever list
// is currently focused, so handleFilterKey doesn't have to branch on pane.
func (m *Model) filterTarget() *string {
	if m.focus == paneServices {
		return &m.svcFilter
	}
	return &m.siteFilter
}

func (m *Model) resetFilteredCursor() {
	if m.focus == paneServices {
		m.svcCursor = 0
		m.svcScroll = 0
		return
	}
	m.siteCursor = 0
	m.siteScroll = 0
}

// syncLogs retargets the log tail to match the currently-focused item
// whenever the log pane is open. Called after every navigation or focus
// change. Resets to the first target of the new selection; previous logCursor
// doesn't transfer since target lists differ per item.
func (m *Model) syncLogs() tea.Cmd {
	if !m.showLogs {
		return nil
	}
	targets := m.currentLogTargets()
	if len(targets) == 0 {
		m.logTail.Stop()
		return nil
	}
	if m.logCursor >= len(targets) {
		m.logCursor = 0
	}
	next := targets[m.logCursor]
	if next.ID == m.logTail.Source() {
		return nil
	}
	m.logCursor = 0
	return m.logTail.Start(targets[0])
}

// nextFocus returns the focus after moving `dir` steps (±1) through the
// list of panes that are currently visible and usable. Skips services when
// hidden and skips detail when no site is selected.
func (m *Model) nextFocus(dir int) focusPane {
	panes := []focusPane{paneSites}
	if !m.hideServices {
		panes = append(panes, paneServices)
	}
	if m.currentSite() != nil {
		panes = append(panes, paneDetail)
	}
	n := len(panes)
	if n == 0 {
		return m.focus
	}
	idx := 0
	for i, p := range panes {
		if p == m.focus {
			idx = i
			break
		}
	}
	idx = ((idx+dir)%n + n) % n
	return panes[idx]
}

func (m *Model) moveCursor(delta int) {
	switch m.focus {
	case paneSites:
		m.siteCursor = clamp(m.siteCursor+delta, 0, max(0, len(m.visibleSites())-1))
		m.closePicker()
	case paneServices:
		m.svcCursor = clamp(m.svcCursor+delta, 0, max(0, len(m.visibleServices())-1))
	case paneDetail:
		if m.pickerKind != kindInfo {
			n := len(m.pickerOptions)
			if n == 0 {
				return
			}
			m.pickerCursor = clamp(m.pickerCursor+delta, 0, n-1)
			return
		}
		switch m.detailMode {
		case detailSettings:
			rows := m.settingsRows()
			m.settingsRow = clamp(m.settingsRow+delta, 0, max(0, len(rows)-1))
		case detailHelp:
			m.helpScroll += delta
			if m.helpScroll < 0 {
				m.helpScroll = 0
			}
		default:
			if s := m.currentSite(); s != nil {
				nav := navigableRows(detailRows(s))
				m.detailCursor = clamp(m.detailCursor+delta, 0, max(0, len(nav)-1))
			}
		}
	}
}

func (m *Model) setCursor(pos int) {
	switch m.focus {
	case paneSites:
		m.siteCursor = clamp(pos, 0, max(0, len(m.visibleSites())-1))
	case paneServices:
		m.svcCursor = clamp(pos, 0, max(0, len(m.visibleServices())-1))
	}
}

func (m *Model) clampCursors() {
	m.siteCursor = clamp(m.siteCursor, 0, max(0, len(m.visibleSites())-1))
	m.svcCursor = clamp(m.svcCursor, 0, max(0, len(m.visibleServices())-1))
}

// visibleSites is the view-ready sites list: m.snap.Sites with the active
// filter and sort applied. Every renderer and cursor lookup goes through
// this so filtered-out rows are invisible to navigation, not just hidden
// visually.
func (m *Model) visibleSites() []siteinfo.EnrichedSite {
	return filteredSortedSites(m.snap.Sites, m.siteFilter, m.siteSort)
}

func (m *Model) visibleServices() []ServiceRow {
	return filteredSortedServices(m.snap.Services, m.svcFilter, m.svcSort)
}

func (m *Model) setStatus(s string, d time.Duration) {
	m.status = s
	m.statusExpiry = time.Now().Add(d)
}

func (m *Model) toggleLogs() tea.Cmd {
	if m.showLogs {
		m.logTail.Stop()
		m.showLogs = false
		return nil
	}
	targets := m.currentLogTargets()
	if len(targets) == 0 {
		m.setStatus("no log source for selected item", 3*time.Second)
		return nil
	}
	m.showLogs = true
	m.logCursor = 0
	return m.logTail.Start(targets[0])
}

// cycleLogTarget steps through the available log sources for the currently
// focused item (FPM → queue → schedule → …). No-op when the log pane is
// closed or the item has only one source.
func (m *Model) cycleLogTarget(delta int) tea.Cmd {
	if !m.showLogs {
		return nil
	}
	targets := m.currentLogTargets()
	if len(targets) <= 1 {
		return nil
	}
	n := len(targets)
	m.logCursor = ((m.logCursor+delta)%n + n) % n
	return m.logTail.Start(targets[m.logCursor])
}

// currentLogTargets returns every log source the user can switch between for
// the focused item. Sites expose their FPM/custom container plus a target
// per running-or-defined worker (each worker is a systemd user unit tailed
// via journalctl --user). Services have just the one container.
func (m *Model) currentLogTargets() []LogTarget {
	switch m.focus {
	case paneSites, paneDetail:
		s := m.currentSite()
		if s == nil {
			return nil
		}
		return logTargetsForSite(s)
	case paneServices:
		svc := m.currentService()
		if svc == nil {
			return nil
		}
		if svc.WorkerKind != "" {
			// Worker-backed services run as systemd user units; tail the
			// journal, matching lerd-ui's handleQueueLogs / handleHorizonLogs.
			return []LogTarget{{
				Kind:  kindJournal,
				ID:    "lerd-" + svc.WorkerKind + "-" + svc.WorkerSite,
				Label: svc.WorkerKind + " · " + svc.WorkerSite,
			}}
		}
		return []LogTarget{{Kind: kindPodman, ID: "lerd-" + svc.Name, Label: svc.Name}}
	}
	return nil
}

func logTargetsForSite(s *siteinfo.EnrichedSite) []LogTarget {
	var out []LogTarget
	if s.ContainerPort > 0 {
		out = append(out, LogTarget{
			Kind:  kindPodman,
			ID:    podman.CustomContainerName(s.Name),
			Label: s.Name + " · container",
		})
	} else if s.PHPVersion != "" {
		out = append(out, LogTarget{
			Kind:  kindPodman,
			ID:    "lerd-php" + strings.ReplaceAll(s.PHPVersion, ".", "") + "-fpm",
			Label: s.Name + " · fpm " + s.PHPVersion,
		})
	}

	addUnit := func(present bool, unitSuffix, label string) {
		if !present {
			return
		}
		out = append(out, LogTarget{
			Kind:  kindJournal,
			ID:    "lerd-" + unitSuffix + "-" + s.Name,
			Label: s.Name + " · " + label,
		})
	}
	addUnit(s.HasQueueWorker, "queue", "queue")
	addUnit(s.HasScheduleWorker, "schedule", "schedule")
	addUnit(s.HasReverb, "reverb", "reverb")
	addUnit(s.HasHorizon, "horizon", "horizon")
	for _, fw := range s.FrameworkWorkers {
		label := fw.Label
		if label == "" {
			label = fw.Name
		}
		out = append(out, LogTarget{
			Kind:  kindJournal,
			ID:    "lerd-" + fw.Name + "-" + s.Name,
			Label: s.Name + " · " + label,
		})
	}
	for _, path := range appLogPathsForSite(s) {
		out = append(out, LogTarget{
			Kind:  kindFile,
			ID:    path,
			Label: s.Name + " · " + filepath.Base(path),
		})
	}
	return out
}

// appLogPathsForSite expands the framework's log-source globs against the
// site's project directory, returning absolute paths. Mirrors the web UI's
// app-logs endpoint: for Laravel that's storage/logs/*.log, other
// frameworks get whatever they declare in their definition.
func appLogPathsForSite(s *siteinfo.EnrichedSite) []string {
	fw, ok := config.GetFrameworkForDir(s.FrameworkName, s.Path)
	if !ok || fw == nil || len(fw.Logs) == 0 {
		return nil
	}
	absProject, err := filepath.Abs(s.Path)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var paths []string
	for _, src := range fw.Logs {
		matches, err := filepath.Glob(filepath.Join(absProject, src.Path))
		if err != nil {
			continue
		}
		for _, m := range matches {
			abs, err := filepath.Abs(m)
			if err != nil || !strings.HasPrefix(abs, absProject+string(filepath.Separator)) {
				continue
			}
			if seen[abs] {
				continue
			}
			if info, err := os.Stat(abs); err != nil || info.IsDir() {
				continue
			}
			seen[abs] = true
			paths = append(paths, abs)
		}
	}
	sort.Strings(paths)
	return paths
}

func (m *Model) currentSite() *siteinfo.EnrichedSite {
	sites := m.visibleSites()
	if m.siteCursor >= 0 && m.siteCursor < len(sites) {
		return &sites[m.siteCursor]
	}
	return nil
}

func (m *Model) currentService() *ServiceRow {
	svcs := m.visibleServices()
	if m.svcCursor >= 0 && m.svcCursor < len(svcs) {
		return &svcs[m.svcCursor]
	}
	return nil
}

func (m *Model) actionStart() tea.Cmd {
	switch m.focus {
	case paneSites, paneDetail:
		if s := m.currentSite(); s != nil {
			m.setStatus("starting "+s.Name+"…", 5*time.Second)
			return runLerd(s.Path, "unpause", s.Name)
		}
	case paneServices:
		if svc := m.currentService(); svc != nil {
			if cmd := m.workerActionCmd(svc, "start"); cmd != nil {
				return cmd
			}
			m.setStatus("starting "+svc.Name+"…", 5*time.Second)
			return runLerd("", "service", "start", svc.Name)
		}
	}
	return nil
}

// workerActionCmd returns a tea.Cmd for start/stop/restart on a worker-
// backed service row. Returns nil for regular service rows so the caller
// can fall through to `lerd service <verb>`.
func (m *Model) workerActionCmd(svc *ServiceRow, verb string) tea.Cmd {
	if svc.WorkerKind == "" || svc.WorkerPath == "" {
		return nil
	}
	m.setStatus(verb+"ing "+svc.WorkerKind+" worker for "+svc.WorkerSite+"…", 5*time.Second)
	switch svc.WorkerKind {
	case "queue", "schedule", "horizon", "reverb":
		if verb == "restart" {
			// Worker restart = stop + start via systemd; we use the unit
			// directly because `lerd queue restart` doesn't exist as a
			// CLI verb on every worker. Two sequenced lerd invocations
			// keep us inside the public CLI.
			return tea.Sequence(
				runLerd(svc.WorkerPath, svc.WorkerKind, "stop"),
				runLerd(svc.WorkerPath, svc.WorkerKind, "start"),
			)
		}
		return runLerd(svc.WorkerPath, svc.WorkerKind, verb)
	default:
		if verb == "restart" {
			return tea.Sequence(
				runLerd(svc.WorkerPath, "worker", "stop", svc.WorkerKind),
				runLerd(svc.WorkerPath, "worker", "start", svc.WorkerKind),
			)
		}
		return runLerd(svc.WorkerPath, "worker", verb, svc.WorkerKind)
	}
}

func (m *Model) actionStop() tea.Cmd {
	switch m.focus {
	case paneSites, paneDetail:
		if s := m.currentSite(); s != nil {
			m.setStatus("pausing "+s.Name+"…", 5*time.Second)
			return runLerd(s.Path, "pause", s.Name)
		}
	case paneServices:
		if svc := m.currentService(); svc != nil {
			if cmd := m.workerActionCmd(svc, "stop"); cmd != nil {
				return cmd
			}
			m.setStatus("stopping "+svc.Name+"…", 5*time.Second)
			return runLerd("", "service", "stop", svc.Name)
		}
	}
	return nil
}

func (m *Model) actionRestart() tea.Cmd {
	switch m.focus {
	case paneSites, paneDetail:
		if s := m.currentSite(); s != nil {
			m.setStatus("restarting "+s.Name+"…", 5*time.Second)
			return runLerd(s.Path, "restart", s.Name)
		}
	case paneServices:
		if svc := m.currentService(); svc != nil {
			if cmd := m.workerActionCmd(svc, "restart"); cmd != nil {
				return cmd
			}
			m.setStatus("restarting "+svc.Name+"…", 5*time.Second)
			return runLerd("", "service", "restart", svc.Name)
		}
	}
	return nil
}

// actionShell opens an interactive shell inside the container that backs
// whatever's currently focused: FPM / custom container for a site, the
// service's own container for a service. Setting the working dir to the
// site's project path means PHP tools (composer, artisan) run as if the
// user had cd'd into the project first.
func (m *Model) actionShell() tea.Cmd {
	switch m.focus {
	case paneSites, paneDetail:
		s := m.currentSite()
		if s == nil {
			return nil
		}
		container := containerForSite(s)
		if container == "" {
			m.setStatus("no container to shell into for "+s.Name, 3*time.Second)
			return nil
		}
		if running, _ := podman.ContainerRunning(container); !running {
			m.setStatus(container+" is not running — start the site first", 4*time.Second)
			return nil
		}
		return runShellIn(container, s.Path)
	case paneServices:
		svc := m.currentService()
		if svc == nil {
			return nil
		}
		if svc.WorkerKind != "" {
			// Worker rows have no container of their own — they run inside
			// the owning site's FPM container. Find that site and shell in
			// there, which is what the user wants when they hit t on e.g.
			// queue-astrolov.
			site := m.siteByName(svc.WorkerSite)
			if site == nil {
				m.setStatus("owner site "+svc.WorkerSite+" not loaded", 3*time.Second)
				return nil
			}
			container := containerForSite(site)
			if container == "" {
				m.setStatus("no container for "+svc.WorkerSite, 3*time.Second)
				return nil
			}
			if running, _ := podman.ContainerRunning(container); !running {
				m.setStatus(container+" is not running", 4*time.Second)
				return nil
			}
			return runShellIn(container, site.Path)
		}
		container := "lerd-" + svc.Name
		if running, _ := podman.ContainerRunning(container); !running {
			m.setStatus(container+" is not running", 4*time.Second)
			return nil
		}
		return runShellIn(container, "")
	}
	return nil
}

// siteByName returns the enriched site in the current snapshot with the
// given name, or nil. Used by worker-service rows that need to reach back
// to their owning site's container and path.
func (m *Model) siteByName(name string) *siteinfo.EnrichedSite {
	for i := range m.snap.Sites {
		if m.snap.Sites[i].Name == name {
			return &m.snap.Sites[i]
		}
	}
	return nil
}

func (m *Model) actionPauseToggle() tea.Cmd {
	if m.focus != paneSites && m.focus != paneDetail {
		return nil
	}
	s := m.currentSite()
	if s == nil {
		return nil
	}
	if s.Paused {
		m.setStatus("resuming "+s.Name+"…", 5*time.Second)
		return runLerd(s.Path, "unpause", s.Name)
	}
	m.setStatus("pausing "+s.Name+"…", 5*time.Second)
	return runLerd(s.Path, "pause", s.Name)
}

// Run starts the bubbletea program with the shared podman cache warmed up.
// Called from cli.NewTuiCmd so the wiring lives next to the rest of the
// command registry.
func Run(version string) error {
	podman.Cache.Start(context.Background())
	m := NewModel(version)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func formatAction(r ActionResult) string {
	if r.Err != nil {
		detail := r.Detail
		if detail == "" {
			detail = r.Err.Error()
		}
		detail = firstLine(detail)
		return fmt.Sprintf("✖ %s: %s", r.Summary, detail)
	}
	return "✓ " + r.Summary
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ensure lipgloss is referenced so keeping it as a named import above doesn't
// trip the linter if the render code moves around.
var _ = lipgloss.NewStyle
