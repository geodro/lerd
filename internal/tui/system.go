package tui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	lerdNode "github.com/geodro/lerd/internal/node"
	phpPkg "github.com/geodro/lerd/internal/php"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	lerdUpdate "github.com/geodro/lerd/internal/update"
)

// systemKind identifies each row in the System detail mode. Non-actionable
// rows use sysHeader / sysInfo so cursor navigation skips them.
type systemKind int

const (
	sysHeader systemKind = iota
	sysInfo
	sysDumpsEnabled
	sysDumpsPassthrough
	sysNotifEnabled
	sysProfiler
	sysAutostart
	sysLANExpose
	sysWorkerMode
	sysXdebug
)

// systemRow is one line in the System detail view. value is shown dimmed on
// the right of label; on drives the on/off glyph for toggle rows. arg holds
// per-row context (currently the PHP version for xdebug rows).
type systemRow struct {
	kind  systemKind
	label string
	value string
	on    bool
	arg   string
}

// systemRows produces every row the System detail view renders. Built per
// frame so live state (DNS status, container running, buffered dump count)
// stays in sync without a separate refresh path.
func (m *Model) systemRows() []systemRow {
	cfg, _ := config.LoadGlobal()
	rows := make([]systemRow, 0, 64)

	add := func(r systemRow) { rows = append(rows, r) }
	header := func(s string) { add(systemRow{kind: sysHeader, label: s}) }
	info := func(label, value string) { add(systemRow{kind: sysInfo, label: label, value: value}) }

	// DNS
	header("DNS")
	tld := "test"
	dnsEnabled := true
	if cfg != nil {
		if cfg.DNS.TLD != "" {
			tld = cfg.DNS.TLD
		}
		dnsEnabled = cfg.DNS.Enabled
	}
	info("TLD", tld)
	if dnsEnabled {
		info("Status", dnsStatusText(dns.CheckStatus(tld)))
	} else {
		info("Status", "disabled (system resolver only)")
	}
	if dns.VPNActive() {
		info("VPN", "active (may bypass lerd-dns)")
	}

	// Nginx
	header("Nginx")
	info("Status", runningOrStopped(m.snap.Status.NginxRunning))

	// Watcher
	header("Watcher")
	info("Status", runningOrStopped(m.snap.Status.WatcherRunning))

	// Notifications
	header("Notifications")
	notifOn := cfg != nil && cfg.IsNotificationsEnabled()
	add(systemRow{kind: sysNotifEnabled, label: "Enabled", on: notifOn})

	// SPX profiler — global toggle that affects every PHP-FPM site.
	header("Profiler")
	profOn := cfg != nil && cfg.IsProfilerEnabled()
	add(systemRow{kind: sysProfiler, label: "SPX profiler", on: profOn})

	// Dump bridge
	header("Dump bridge")
	dumpsOn := cfg != nil && cfg.IsDumpsEnabled()
	add(systemRow{kind: sysDumpsEnabled, label: "Enabled", on: dumpsOn})
	add(systemRow{kind: sysDumpsPassthrough, label: "Passthrough", on: cfg != nil && cfg.IsDumpsPassthrough()})
	info("Listen", config.DumpsListenAddr())
	info("Buffered", fmt.Sprintf("%d events (cap %d)", len(m.dumps), dumpsBufferCap))

	// PHP versions
	header("PHP versions")
	defaultPHP := ""
	if cfg != nil {
		defaultPHP = cfg.PHP.DefaultVersion
	}
	if defaultPHP != "" {
		info("Default", defaultPHP)
	}
	versions, _ := phpPkg.ListInstalled()
	if len(versions) == 0 {
		info("Installed", "none")
	}
	runningSet := map[string]bool{}
	for _, v := range m.snap.Status.PHPRunning {
		runningSet[v] = true
	}
	for _, v := range versions {
		state := "FPM stopped"
		if runningSet[v] {
			state = "FPM running"
		}
		if v == defaultPHP {
			state += " · default"
		}
		info("PHP "+v, state)
		on := cfg != nil && cfg.IsXdebugEnabled(v)
		mode := ""
		if cfg != nil {
			mode = cfg.GetXdebugMode(v)
		}
		label := "Xdebug · PHP " + v
		if on && mode != "" {
			label += " (" + mode + ")"
		}
		add(systemRow{kind: sysXdebug, label: label, on: on, arg: v})
	}

	// Node
	header("Node")
	defaultNode := ""
	if cfg != nil {
		defaultNode = cfg.Node.DefaultVersion
	}
	if defaultNode != "" {
		info("Default", defaultNode)
	} else {
		info("Default", "(none)")
	}
	if nodeVersions := lerdNode.ListInstalled(); len(nodeVersions) > 0 {
		info("Installed", strings.Join(nodeVersions, ", "))
	} else {
		info("Installed", "none (run `lerd node:install <ver>`)")
	}

	// Worker mode (macOS only — on Linux every worker is exec-mode under systemd)
	if runtime.GOOS == "darwin" {
		header("Worker mode")
		containerMode := cfg != nil && cfg.WorkerExecMode() == config.WorkerExecModeContainer
		label := "Container mode (one container per worker)"
		if !containerMode {
			label = "Exec mode (shared FPM container)"
		}
		add(systemRow{kind: sysWorkerMode, label: label, on: containerMode})
	}

	// Lerd
	header("Lerd")
	info("Version", m.version)
	if m.updateAvailable != "" {
		info("Update", m.updateAvailable+" available (run `lerd update`)")
	} else {
		if latest, _ := lerdUpdate.CachedUpdateCheck(m.version); latest != nil && latest.LatestVersion != "" {
			info("Update", "you are on the latest ("+latest.LatestVersion+")")
		} else {
			info("Update", "no cached check yet")
		}
	}
	add(systemRow{kind: sysAutostart, label: "Autostart on login", on: lerdSystemd.IsAutostartEnabled()})
	add(systemRow{kind: sysLANExpose, label: "LAN expose (every service)", on: cfg != nil && cfg.LAN.Exposed})

	return rows
}

// navigableSystemRows returns the indices of focusable rows, skipping
// section headers and info-only rows. Lets the cursor land only on
// interactive items.
func navigableSystemRows(rows []systemRow) []int {
	out := make([]int, 0, len(rows))
	for i, r := range rows {
		if r.kind == sysHeader || r.kind == sysInfo {
			continue
		}
		out = append(out, i)
	}
	return out
}

// systemToggle dispatches the action for the focused row. Mirrors
// settingsToggle's shape: every branch shells out to the public CLI so the
// TUI shares the same code paths as a manual `lerd ...` invocation.
func (m *Model) systemToggle(rows []systemRow) tea.Cmd {
	nav := navigableSystemRows(rows)
	if len(nav) == 0 {
		return nil
	}
	if m.systemRow >= len(nav) {
		m.systemRow = len(nav) - 1
	}
	row := rows[nav[m.systemRow]]
	switch row.kind {
	case sysDumpsEnabled:
		verb := "on"
		if row.on {
			verb = "off"
		}
		m.setStatus("dump bridge "+verb+"…", 5*time.Second)
		return runLerd("", "dump", verb)
	case sysDumpsPassthrough:
		// No public CLI verb yet; surface the state but skip toggling.
		// Leaving as a no-op is preferable to inventing a side-channel
		// config write because dumps passthrough also requires the FPM
		// bridge ini to be rewritten, which `dumpsops.Apply` handles.
		m.setStatus("dumps passthrough: toggle via lerd-ui dashboard", 3*time.Second)
		return nil
	case sysNotifEnabled:
		verb := "on"
		if row.on {
			verb = "off"
		}
		m.setStatus("notifications "+verb+"…", 5*time.Second)
		return runLerd("", "notify", verb)
	case sysProfiler:
		verb := "on"
		if row.on {
			verb = "off"
		}
		m.setStatus("profiler "+verb+"…", 5*time.Second)
		return runLerd("", "profile", verb)
	case sysAutostart:
		sub := "enable"
		if row.on {
			sub = "disable"
		}
		m.setStatus("autostart "+sub+"…", 5*time.Second)
		return runLerd("", "autostart", sub)
	case sysLANExpose:
		verb := "on"
		if row.on {
			verb = "off"
		}
		m.setStatus("LAN expose "+verb+"…", 5*time.Second)
		return runLerd("", "lan", "expose", verb)
	case sysWorkerMode:
		target := config.WorkerExecModeContainer
		if row.on {
			target = config.WorkerExecModeExec
		}
		m.setStatus("switching worker mode to "+target+"…", 5*time.Second)
		return runLerd("", "workers", "mode", target)
	case sysXdebug:
		verb := "on"
		if row.on {
			verb = "off"
		}
		m.setStatus("xdebug "+verb+" PHP "+row.arg+"…", 5*time.Second)
		return runLerd("", "xdebug", verb, row.arg)
	}
	return nil
}

// systemContentLinesWithCursor renders the System detail pane and reports the
// rendered-line index of the currently-selected row so the viewport can keep
// the cursor on screen. Headers are bold, info rows are dim, toggleable rows
// show an on/off glyph plus state text. Mirrors settingsContentLines' clip/
// pad convention so the border wraps cleanly at the right edge.
func systemContentLinesWithCursor(m *Model, focused bool, innerW int) ([]string, int) {
	rows := m.systemRows()
	nav := navigableSystemRows(rows)
	navPos := func(i int) int {
		for pos, idx := range nav {
			if idx == i {
				return pos
			}
		}
		return -1
	}

	out := make([]string, 0, len(rows)+4)
	cursorLine := 0
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	add(sectionStyle.Render("System"))
	add(dimStyle.Render("  press Y or esc to return to site detail"))
	add("")

	for i, row := range rows {
		switch row.kind {
		case sysHeader:
			add("")
			add(sectionStyle.Render(row.label))
		case sysInfo:
			add(renderSystemInfoRow(row.label, row.value))
		default:
			selected := focused && navPos(i) == m.systemRow
			if selected {
				cursorLine = len(out)
			}
			add(renderDetailRow(selected, onOffGlyph(row.on), row.label, onOffText(row.on)))
		}
	}
	return out, cursorLine
}

// renderSystemInfoRow formats a left-label / right-value line, e.g.
// "  TLD                 test". Keeps padding consistent with toggle rows
// so the columns line up.
func renderSystemInfoRow(label, value string) string {
	padded := label
	if w := len([]rune(label)); w < 18 {
		padded = label + spaces(18-w)
	}
	return "    " + dimStyle.Render(padded) + " " + value
}

// dnsStatusText converts dns.Status into the user-facing string shown in the
// status row. Centralised so both the system page and a future System
// dashboard widget render the same text.
func dnsStatusText(s dns.Status) string {
	switch s {
	case dns.StatusOK:
		return "ok"
	case dns.StatusDegraded:
		return "degraded (lerd-dns up, system resolver bypassed)"
	default:
		return "down"
	}
}

func runningOrStopped(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
}
