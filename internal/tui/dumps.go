package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/config"
	lerddumps "github.com/geodro/lerd/internal/dumps"
)

// toggleString returns "" when current already equals target (clearing
// the filter) and target otherwise. Used by the chip handlers in the
// dumps view so a second press of `1` or `2` turns the filter off.
func toggleString(current, target string) string {
	if current == target {
		return ""
	}
	return target
}

// dumpsBufferCap is the max number of dump entries the TUI keeps in memory.
// Sized lower than the lerd-ui ring (500) because TUI rendering is per-frame
// and visible viewport is small; older events scroll off the visible list.
const dumpsBufferCap = 200

// dumpEventMsg is delivered to Update when the SSE listener gets a new event.
type dumpEventMsg DumpEntry

// runDumpsListener opens the lerd-ui Unix-socket SSE endpoint and pumps
// parsed events into the bubbletea program. Reconnects with backoff so the
// TUI keeps refreshing across lerd-ui restarts. Cancelled by ctx.
func runDumpsListener(ctx context.Context, p *tea.Program) {
	backoff := 500 * time.Millisecond
	for ctx.Err() == nil {
		if err := streamDumpsOnce(ctx, p); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 10*time.Second {
				backoff *= 2
			}
			continue
		}
		// Clean disconnect (lerd-ui exited cleanly): retry sooner.
		backoff = 500 * time.Millisecond
	}
}

func streamDumpsOnce(ctx context.Context, p *tea.Program) error {
	conn, err := net.DialTimeout("unix", config.UISocketPath(), 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	req, _ := http.NewRequestWithContext(ctx, "GET", "http://lerd/api/dumps/stream", nil)
	req.Header.Set("Accept", "text/event-stream")
	if err := req.Write(conn); err != nil {
		return err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %s", resp.Status)
	}

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), lerddumps.MaxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var ev lerddumps.Event
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			continue
		}
		p.Send(dumpEventMsg(toDumpEntry(ev)))
	}
	return scanner.Err()
}

func toDumpEntry(ev lerddumps.Event) DumpEntry {
	return DumpEntry{
		ID:      ev.ID,
		TS:      ev.TS,
		Type:    ev.Ctx.Type,
		Site:    ev.Ctx.Site,
		Request: ev.Ctx.Request,
		File:    ev.Src.File,
		Line:    ev.Src.Line,
		Label:   ev.Label,
		Text:    ev.Text,
	}
}

// appendDump folds a new event into the model, capping the buffer at
// dumpsBufferCap and de-duping on ID (replays from the SSE replay path).
// Cursor 0 in the dumps view always targets the newest entry; if the user
// has scrolled off newest (cursor > 0) we bump their cursor by one when a
// fresh dump lands so the marker keeps pointing at the same dump rather
// than silently slipping to the row that just moved into place.
func (m *Model) appendDump(e DumpEntry) {
	for _, existing := range m.dumps {
		if existing.ID == e.ID {
			return
		}
	}
	m.dumps = append(m.dumps, e)
	if len(m.dumps) > dumpsBufferCap {
		m.dumps = m.dumps[len(m.dumps)-dumpsBufferCap:]
	}
	if m.dumpsCursor > 0 {
		m.dumpsCursor++
		if m.dumpsCursor >= len(m.dumps) {
			m.dumpsCursor = len(m.dumps) - 1
		}
	}
}

// dumpsContentLines builds the lines rendered when detailMode == detailDumps.
// Each entry shows a one-line header (time, ctx, site, request) plus a
// brief preview, or the full text when expanded. j/k scrolls through
// entries; enter/space toggles expansion of the focused entry; / filters by
// substring across site/request/label/file/text; c clears the buffer.
// cursorLine is returned so the viewport keeps the selection on screen.
func dumpsContentLines(m *Model, focused bool, innerW int) ([]string, int) {
	out := make([]string, 0, len(m.dumps)*4+8)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	filtered := filteredDumpsWithCtx(m.dumps, m.dumpsFilter, m.dumpsCtxFilter)
	add(sectionStyle.Render("Dumps") + "  " + dimStyle.Render(dumpsBridgeStateLabel()))

	header := fmt.Sprintf("  %d shown / %d buffered (cap %d)", len(filtered), len(m.dumps), dumpsBufferCap)
	add(dimStyle.Render(header))
	add(dimStyle.Render("  / search · 1/2 ctx chips · enter expand · c clear · T toggle bridge · D return"))
	add("  " + renderDumpsChips(m.dumpsCtxFilter))

	if m.dumpsFilterActive || m.dumpsFilter != "" {
		add(padToWidth(filterBar(m.dumpsFilter, m.dumpsFilterActive), innerW))
	}
	add("")

	if len(m.dumps) == 0 {
		add(dimStyle.Render("  no dumps yet"))
		add("")
		add("  " + dimStyle.Render("1. enable the bridge with ") + accentStyle.Render("T") + dimStyle.Render(" or ") + accentStyle.Render("lerd dump on"))
		add("  " + dimStyle.Render("2. trigger a ") + accentStyle.Render("dump()") + dimStyle.Render(" or ") + accentStyle.Render("dd()") + dimStyle.Render(" call in your PHP code"))
		add("  " + dimStyle.Render("3. events stream into this pane as they arrive"))
		return out, 0
	}
	if len(filtered) == 0 {
		add(dimStyle.Render("  no dumps match this filter"))
		return out, 0
	}

	if m.dumpsCursor < 0 {
		m.dumpsCursor = 0
	}
	if m.dumpsCursor >= len(filtered) {
		m.dumpsCursor = len(filtered) - 1
	}
	cursorLine := 3

	// Newest first for at-a-glance most-recent-on-top, and cursor 0 also
	// targets the newest entry so j/k feel right: down moves the marker
	// from newest at the top toward older entries below. The render loop
	// walks the slice backwards but compares against a converted "row
	// index from the top" rather than the slice index, so the cursor
	// direction stays intuitive regardless of how the buffer is laid out.
	for row, i := 0, len(filtered)-1; i >= 0; i-- {
		e := filtered[i]
		marker := "  "
		if row == m.dumpsCursor {
			marker = "▶ "
			cursorLine = len(out)
		}
		hdr := marker + dumpHeaderLine(e)
		if focused && row == m.dumpsCursor {
			hdr = selectedStyle.Render(hdr)
		}
		add(hdr)
		expanded := m.dumpsExpanded != nil && m.dumpsExpanded[e.ID]
		body := dumpBodyLines(e, innerW-4, expanded)
		for _, ln := range body {
			add("    " + dimStyle.Render(ln))
		}
		add("")
		row++
	}
	return out, cursorLine
}

// filteredDumps returns entries that match needle across any of the
// human-meaningful fields (site, request, label, file, text). Case-
// insensitive; empty needle returns the input unchanged so callers don't
// pay the copy cost. ctx, when set, further restricts to entries whose
// Type matches ("fpm" or "cli") — wired to the chip toggles in the dumps
// header.
func filteredDumps(in []DumpEntry, needle string) []DumpEntry {
	return filteredDumpsWithCtx(in, needle, "")
}

func filteredDumpsWithCtx(in []DumpEntry, needle, ctx string) []DumpEntry {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" && ctx == "" {
		return in
	}
	out := make([]DumpEntry, 0, len(in))
	for _, e := range in {
		if ctx != "" && !strings.EqualFold(e.Type, ctx) {
			continue
		}
		if needle != "" && !dumpMatches(e, needle) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func dumpMatches(e DumpEntry, needle string) bool {
	if strings.Contains(strings.ToLower(e.Site), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Request), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Label), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(e.File), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Text), needle) {
		return true
	}
	if strings.Contains(strings.ToLower(e.Type), needle) {
		return true
	}
	return false
}

// dumpBodyLines returns the lines to render below an entry header: a brief
// preview by default (first 4 lines), or the full text when expanded.
// Wraps long lines at width; expanded entries render every line.
func dumpBodyLines(e DumpEntry, width int, expanded bool) []string {
	if e.Text == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(e.Text, "\n"), "\n")
	if !expanded {
		const maxPreview = 4
		if len(lines) > maxPreview {
			lines = append(lines[:maxPreview], fmt.Sprintf("… (%d more lines · enter to expand)", len(lines)-maxPreview))
		}
	}
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if width > 0 && len(ln) > width {
			ln = ln[:width-1] + "…"
		}
		out = append(out, ln)
	}
	return out
}

// renderDumpsChips formats the FPM / CLI context-filter chips that sit
// under the dumps header. Active chip wears the accent background; the
// inactive one shows in dim text so the user knows the toggle exists.
// The "all" chip is implicit — both off means everything is shown.
func renderDumpsChips(active string) string {
	chip := func(label, key string, on bool) string {
		text := " " + key + " " + label + " "
		if on {
			return keyChipStyle.Render(text)
		}
		return dimStyle.Render("[ " + key + " " + label + " ]")
	}
	return chip("fpm", "1", active == "fpm") + "  " + chip("cli", "2", active == "cli")
}

// dumpsBridgeStateLabel returns a coloured "bridge on/off" label for the
// dumps header. Reads the canonical config every call so the indicator
// updates as soon as the user toggles with T.
func dumpsBridgeStateLabel() string {
	cfg, _ := config.LoadGlobal()
	if cfg != nil && cfg.IsDumpsEnabled() {
		return runningStyle.Render("bridge on")
	}
	return failingStyle.Render("bridge off") + dimStyle.Render(" (T to enable)")
}

// toggleDumpExpand flips the expansion state for the currently-selected
// dump entry, returning no command (the next render reads the new state).
// Cursor 0 means the newest entry, which sits at filtered[len-1] in
// insertion order — invert when indexing.
func (m *Model) toggleDumpExpand() tea.Cmd {
	filtered := filteredDumpsWithCtx(m.dumps, m.dumpsFilter, m.dumpsCtxFilter)
	if len(filtered) == 0 || m.dumpsCursor < 0 || m.dumpsCursor >= len(filtered) {
		return nil
	}
	if m.dumpsExpanded == nil {
		m.dumpsExpanded = map[string]bool{}
	}
	idx := len(filtered) - 1 - m.dumpsCursor
	id := filtered[idx].ID
	m.dumpsExpanded[id] = !m.dumpsExpanded[id]
	return nil
}

// clearDumps stages a confirm modal so a stray `c` keypress doesn't wipe
// buffered events the user is actively reading. On y the local buffer is
// zeroed and `lerd dump clear` runs against the daemon ring. Matches the
// removeFocusedDomain pattern: single-key destructive actions go through
// openConfirm so the policy is consistent across the TUI.
func (m *Model) clearDumps() tea.Cmd {
	count := len(m.dumps)
	if count == 0 {
		// Empty buffer — go straight to the daemon clear without a prompt
		// since there's nothing local to lose.
		m.setStatus("cleared dump buffer…", 3*time.Second)
		return runLerd("", "dump", "clear")
	}
	body := fmt.Sprintf("Drop %d buffered events from the dashboard and run `lerd dump clear` against the daemon ring? This cannot be undone.", count)
	m.openConfirm("Clear dumps", body, tea.Sequence(
		func() tea.Msg {
			m.dumps = nil
			m.dumpsExpanded = nil
			m.dumpsCursor = 0
			m.dumpsScroll = 0
			return nil
		},
		runLerd("", "dump", "clear"),
	))
	return nil
}

// toggleDumpsBridge runs `lerd dump on` / `lerd dump off` based on the
// current state. The header label reads the config directly so the change
// is visible on the next refresh tick once the subprocess writes config.
func (m *Model) toggleDumpsBridge() tea.Cmd {
	cfg, _ := config.LoadGlobal()
	enabled := cfg != nil && cfg.IsDumpsEnabled()
	verb := "on"
	if enabled {
		verb = "off"
	}
	m.setStatus("dump bridge "+verb+"…", 5*time.Second)
	return runLerd("", "dump", verb)
}

func dumpHeaderLine(e DumpEntry) string {
	t := shortTime(e.TS)
	parts := []string{t, e.Type}
	if e.Site != "" {
		parts = append(parts, e.Site)
	}
	if e.Request != "" {
		parts = append(parts, e.Request)
	}
	if e.Label != "" {
		parts = append(parts, "$"+e.Label)
	}
	if e.File != "" {
		parts = append(parts, fmt.Sprintf("%s:%d", shortPath(e.File), e.Line))
	}
	return strings.Join(parts, "  ")
}

func dumpPreviewLines(e DumpEntry, width int) []string {
	if e.Text == "" {
		return nil
	}
	const maxPreview = 4
	lines := strings.Split(strings.TrimRight(e.Text, "\n"), "\n")
	if len(lines) > maxPreview {
		lines = append(lines[:maxPreview], fmt.Sprintf("… (%d more lines)", len(lines)-maxPreview))
	}
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if width > 0 && len(ln) > width {
			ln = ln[:width-1] + "…"
		}
		out = append(out, ln)
	}
	return out
}

func shortTime(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		// Fallback: cut to HH:MM:SS portion if present.
		if len(ts) >= 19 && ts[10] == 'T' {
			return ts[11:19]
		}
		return ts
	}
	return t.Local().Format("15:04:05")
}

func shortPath(p string) string {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(parts) <= 3 {
		return p
	}
	return ".../" + strings.Join(parts[len(parts)-3:], "/")
}
