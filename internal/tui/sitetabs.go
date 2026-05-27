package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/siteinfo"
)

// siteTab identifies which sub-view of Site detail is showing. Tabs let the
// detail pane mirror the web UI's Overview / Env / Dumps / App logs split
// without forcing users to scroll past the toggles every time they want to
// inspect a different facet of the site.
type siteTab int

const (
	tabSiteOverview siteTab = iota
	tabSiteEnv
	tabSiteDumps
	tabSiteAppLogs
)

// envReadLimit caps how much of a site's .env we slurp. 256 KB is two
// orders of magnitude above a realistic .env and small enough that a
// pathological file never wedges the render loop.
const envReadLimit = 256 * 1024

// siteTabLabel returns the title shown in the tab strip header.
func siteTabLabel(t siteTab) string {
	switch t {
	case tabSiteEnv:
		return "Env"
	case tabSiteDumps:
		return "Dumps"
	case tabSiteAppLogs:
		return "App logs"
	default:
		return "Overview"
	}
}

// siteTabsHeader renders the tab strip across the top of the site detail
// pane, e.g. "[1] Overview · [2] Env · [3] Dumps · [4] App logs". The active
// tab is highlighted in the accent colour; the others are dimmed. Lives at
// the head of every site detail variant so the user always sees the
// shortcuts and which tab is active without scrolling.
func siteTabsHeader(active siteTab) string {
	tabs := []siteTab{tabSiteOverview, tabSiteEnv, tabSiteDumps, tabSiteAppLogs}
	parts := make([]string, 0, len(tabs))
	for i, t := range tabs {
		label := fmt.Sprintf("[%d] %s", i+1, siteTabLabel(t))
		if t == active {
			parts = append(parts, selectedStyle.Render(label))
		} else {
			parts = append(parts, dimStyle.Render(label))
		}
	}
	return "  " + strings.Join(parts, "  ")
}

// renderSiteTabHeader returns the two-line block that precedes every site
// tab's content: the tab strip and a divider. Centralised so each tab
// renderer pads to the same width and the user sees a consistent header.
func renderSiteTabHeader(active siteTab, innerW int) []string {
	return []string{
		padToWidth(clipLine(siteTabsHeader(active), innerW), innerW),
		"",
	}
}

// siteEnvContentLines reads the site's .env file and renders one line per
// row. Read-only (matches the web UI's SiteEnvTab in PR1; an editor lands
// in a later phase). Empty .env or missing file renders a helpful empty-
// state so users understand the file isn't on disk yet.
func siteEnvContentLines(m *Model, site *siteinfo.EnrichedSite, innerW int) []string {
	out := make([]string, 0, 32)
	out = append(out, renderSiteTabHeader(tabSiteEnv, innerW)...)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	if site == nil {
		add(dimStyle.Render("  no site selected"))
		return out
	}

	envPath := filepath.Join(site.Path, ".env")
	add(sectionStyle.Render(".env") + "  " + dimStyle.Render(envPath))
	add("")

	data, err := readBoundedFile(envPath, envReadLimit)
	switch {
	case os.IsNotExist(err):
		add(dimStyle.Render("  no .env on disk yet"))
		return out
	case err != nil:
		add(failingStyle.Render("  read error: ") + err.Error())
		return out
	case len(data) == 0:
		add(dimStyle.Render("  .env is empty"))
		return out
	}

	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		add("  " + line)
	}
	return out
}

// siteDumpsContentLines filters the global dump buffer down to events whose
// Site matches the focused site, then renders them with the same chrome the
// global Dumps view uses. Mirrors what the web UI's per-site Dumps tab
// shows: only this site's emissions, in newest-first order.
func siteDumpsContentLines(m *Model, site *siteinfo.EnrichedSite, innerW int) []string {
	out := make([]string, 0, 32)
	out = append(out, renderSiteTabHeader(tabSiteDumps, innerW)...)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	if site == nil {
		add(dimStyle.Render("  no site selected"))
		return out
	}

	// Restrict to this site first, then route through the same global
	// pipeline so the chip filter (m.dumpsCtxFilter) and search needle
	// (m.dumpsFilter) the user already set on the D view apply here too.
	scoped := make([]DumpEntry, 0, len(m.dumps))
	for _, d := range m.dumps {
		if d.Site == site.Name {
			scoped = append(scoped, d)
		}
	}
	matching := filteredDumpsWithCtx(scoped, m.dumpsFilter, m.dumpsCtxFilter)

	add(sectionStyle.Render("Dumps for " + site.Name))
	hint := ""
	if m.dumpsCtxFilter != "" {
		hint += " · ctx:" + m.dumpsCtxFilter
	}
	if needle := strings.TrimSpace(m.dumpsFilter); needle != "" {
		hint += " · /" + needle
	}
	add(dimStyle.Render(fmt.Sprintf("  %d of %d match this site%s", len(matching), len(scoped), hint)))
	add("")

	if len(matching) == 0 {
		if len(scoped) == 0 {
			add(dimStyle.Render("  no dumps from this site yet"))
			add("")
			add("  " + dimStyle.Render("press ") + accentStyle.Render("D") + dimStyle.Render(" to open the global dumps view, then ") + accentStyle.Render("T") + dimStyle.Render(" to enable the bridge"))
			add("  " + dimStyle.Render("dumps from ") + accentStyle.Render(site.Name) + dimStyle.Render(" will appear here automatically"))
		} else {
			add(dimStyle.Render("  no dumps match the active filter for this site"))
			add("  " + dimStyle.Render("clear from the global D view: ") + accentStyle.Render("/") + dimStyle.Render(" for search, ") + accentStyle.Render("1") + dimStyle.Render("/") + accentStyle.Render("2") + dimStyle.Render(" for chips"))
		}
		return out
	}

	for i := len(matching) - 1; i >= 0; i-- {
		e := matching[i]
		add(dumpHeaderLine(e))
		for _, ln := range dumpPreviewLines(e, innerW-4) {
			add("    " + dimStyle.Render(ln))
		}
		add("")
	}
	return out
}

// siteAppLogsContentLines lists every tail-able file behind the focused
// site (framework-declared app logs) with its size and modification time.
// Informational only; users press `l` to actually tail one — the file
// targets are wired into logTargetsForSite so `l` / `[` / `]` already do
// the right thing.
func siteAppLogsContentLines(m *Model, site *siteinfo.EnrichedSite, innerW int) []string {
	out := make([]string, 0, 32)
	out = append(out, renderSiteTabHeader(tabSiteAppLogs, innerW)...)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	if site == nil {
		add(dimStyle.Render("  no site selected"))
		return out
	}

	add(sectionStyle.Render("App logs"))
	add(dimStyle.Render("  press l to tail · [ / ] to cycle through targets"))
	add("")

	paths := appLogPathsForSite(site)
	if len(paths) == 0 {
		add(dimStyle.Render("  no app log paths declared for this framework"))
		add("")
		add("  " + dimStyle.Render("for Laravel: ") + accentStyle.Render("storage/logs/*.log") + dimStyle.Render(" once the app starts writing them"))
		add("  " + dimStyle.Render("for FPM containers: press ") + accentStyle.Render("l") + dimStyle.Render(" to tail the container log instead"))
		return out
	}

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			add(failingStyle.Render("  ! ") + p + "  " + dimStyle.Render(err.Error()))
			continue
		}
		size := humanSize(info.Size())
		mtime := info.ModTime().Local().Format("15:04:05")
		short := filepath.Base(p)
		add("  " + accentStyle.Render("·") + " " +
			padRight(truncatePlain(short, 30), 30) + " " +
			dimStyle.Render(padRight(size, 9)) + " " +
			dimStyle.Render(mtime) + "  " +
			dimStyle.Render(p))
	}
	return out
}

// readBoundedFile reads up to max bytes of path. Used for the env reader so
// a runaway file (cat /dev/zero > .env) can't OOM the TUI.
func readBoundedFile(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, max)
	n, err := f.Read(buf)
	if err != nil && err.Error() != "EOF" {
		return buf[:n], err
	}
	return buf[:n], nil
}

// humanSize formats a byte count as the smallest unit ≥1: "512B", "12KB",
// "3.4MB". Kept terser than stats.FormatBytes so the file-list column stays
// narrow when several logs share a row.
func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.0fKB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1fGB", float64(n)/(1024*1024*1024))
	}
}

// openInBrowserCmd shells out to xdg-open / open with the primary domain
// of the focused site. Falls back to a status-bar message when no domain
// is set or the platform lacks a known opener. Returns nil when the focus
// isn't on a site so other key handlers can carry on.
func (m *Model) openInBrowserCmd() tea.Cmd {
	site := m.currentSite()
	if site == nil {
		return nil
	}
	domain := site.PrimaryDomain()
	if domain == "" {
		m.setStatus("no domain to open for "+site.Name, 3*time.Second)
		return nil
	}
	scheme := "http"
	if site.Secured {
		scheme = "https"
	}
	url := scheme + "://" + domain
	opener := browserOpener()
	if opener == "" {
		m.setStatus("no browser opener available on "+runtime.GOOS, 3*time.Second)
		return nil
	}
	m.setStatus("opening "+url+"…", 3*time.Second)
	return func() tea.Msg {
		cmd := exec.Command(opener, url)
		runErr := cmd.Start()
		// Don't wait — xdg-open returns quickly, the browser detaches.
		return ActionResult{
			Summary: "open " + url,
			Err:     runErr,
		}
	}
}

// browserOpener picks the platform command that launches the default
// browser. Linux uses xdg-open, macOS uses open. Returns "" on platforms
// where neither is appropriate so the caller surfaces a status message
// instead of erroring.
func browserOpener() string {
	switch runtime.GOOS {
	case "darwin":
		return "open"
	case "linux":
		return "xdg-open"
	default:
		return ""
	}
}
