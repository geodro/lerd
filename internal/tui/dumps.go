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
	"github.com/geodro/lerd/internal/dumps"
)

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
	scanner.Buffer(make([]byte, 64*1024), dumps.MaxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var ev dumps.Event
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			continue
		}
		p.Send(dumpEventMsg(toDumpEntry(ev)))
	}
	return scanner.Err()
}

func toDumpEntry(ev dumps.Event) DumpEntry {
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
}

// dumpsContentLines builds the lines rendered when detailMode == detailDumps.
// Each entry shows a one-line header (time, ctx, site, request) plus a
// brief preview of the dumper text. j/k scroll through entries; expanding
// (enter) is intentionally omitted in PR1 — a future revision can add a
// per-entry expand. cursorLine is returned so the viewport can keep the
// selection on screen.
func dumpsContentLines(m *Model, focused bool, innerW int) ([]string, int) {
	out := make([]string, 0, len(m.dumps)*4+8)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	add(sectionStyle.Render("Dumps"))
	add(dimStyle.Render(fmt.Sprintf("  %d buffered (cap %d), press D to return", len(m.dumps), dumpsBufferCap)))
	add("")

	if len(m.dumps) == 0 {
		add(dimStyle.Render("  no dumps yet"))
		add(dimStyle.Render("  enable with `lerd dump on` and trigger a dump() / dd() call"))
		return out, 0
	}

	if m.dumpsCursor < 0 {
		m.dumpsCursor = 0
	}
	if m.dumpsCursor >= len(m.dumps) {
		m.dumpsCursor = len(m.dumps) - 1
	}
	cursorLine := 3

	// Newest first for at-a-glance most-recent-on-top.
	for i := len(m.dumps) - 1; i >= 0; i-- {
		e := m.dumps[i]
		marker := "  "
		if i == m.dumpsCursor {
			marker = "▶ "
			cursorLine = len(out)
		}
		header := marker + dumpHeaderLine(e)
		if focused && i == m.dumpsCursor {
			header = selectedStyle.Render(header)
		}
		add(header)
		for _, ln := range dumpPreviewLines(e, innerW-4) {
			add("    " + dimStyle.Render(ln))
		}
		add("")
	}
	return out, cursorLine
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
