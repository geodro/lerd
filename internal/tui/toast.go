package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Toast notifications: lightweight right-aligned banners stacked above the
// footer. They replace the prior disappearing status bar for action
// results, mirroring herdr's pattern of "coloured dot encodes severity,
// stays visible until dismissed or auto-expired".
//
// Persistence: max maxVisibleToasts shown at once (oldest evicted), each
// auto-expires after toastTTL. The user can dismiss the newest with `d`.

const (
	maxVisibleToasts = 3
	// Per-kind TTLs: success acknowledgements vanish fast (~3s, like a
	// macOS notification), warnings get a bit longer to read, failures
	// stay long enough that the user has time to read the error before
	// it disappears. `d` always dismisses early if they want.
	toastTTLSuccess = 3 * time.Second
	toastTTLWarn    = 6 * time.Second
	toastTTLFail    = 10 * time.Second
	// maxToastWidth caps the toast width so it doesn't span the whole
	// terminal on wide screens — keeps the corner feeling like a corner.
	maxToastWidth = 60
)

// toastTTL returns the auto-expire window for a given severity. Centralised
// so pruneExpiredToasts and any future hover-to-pause logic share the same
// per-kind durations.
func toastTTL(k toastKind) time.Duration {
	switch k {
	case toastFail:
		return toastTTLFail
	case toastWarn:
		return toastTTLWarn
	}
	return toastTTLSuccess
}

// toastKind selects the dot colour. Three buckets match the action-result
// lifecycle: success (ran cleanly), warn (ran but produced a hint), fail
// (errored). Mapped from ActionResult by enqueueToastForResult.
type toastKind int

const (
	toastSuccess toastKind = iota
	toastWarn
	toastFail
)

// toast is one notification entry. ts is the moment it was queued; the
// auto-expiry compare is ts + toastTTL.
type toast struct {
	kind  toastKind
	title string
	body  string
	ts    time.Time
}

// enqueueToast appends a new toast and trims the oldest if we've exceeded
// maxVisibleToasts. Same-title back-to-back enqueues are coalesced to
// avoid a wall of identical "starting redis…" entries when a user is
// mashing keys.
func (m *Model) enqueueToast(kind toastKind, title, body string) {
	if n := len(m.toasts); n > 0 {
		last := m.toasts[n-1]
		if last.kind == kind && last.title == title && last.body == body {
			// Refresh the timestamp so the dupe extends rather than
			// stacks.
			m.toasts[n-1].ts = time.Now()
			return
		}
	}
	m.toasts = append(m.toasts, toast{kind: kind, title: title, body: body, ts: time.Now()})
	if len(m.toasts) > maxVisibleToasts {
		m.toasts = m.toasts[len(m.toasts)-maxVisibleToasts:]
	}
}

// dismissNewestToast removes the most recent toast; `d` keypress wired
// through handleMainKey. No-op when no toasts are active.
func (m *Model) dismissNewestToast() {
	if len(m.toasts) == 0 {
		return
	}
	m.toasts = m.toasts[:len(m.toasts)-1]
}

// pruneExpiredToasts drops entries older than their per-kind TTL. Called
// from the spinner tick handler so the cleanup runs ~10x a second without
// a dedicated timer; the work is O(maxVisibleToasts). Success toasts get
// the shortest window so they don't pile up; failures linger so the user
// has time to read the error.
func (m *Model) pruneExpiredToasts() {
	if len(m.toasts) == 0 {
		return
	}
	now := time.Now()
	out := m.toasts[:0]
	for _, t := range m.toasts {
		if now.Sub(t.ts) < toastTTL(t.kind) {
			out = append(out, t)
		}
	}
	m.toasts = out
}

// enqueueToastForResult converts an ActionResult into a toast. Success
// gets a green dot + plain title; errors get a red dot + first line of
// stderr. Callers don't have to think about styling.
func (m *Model) enqueueToastForResult(r ActionResult) {
	if r.Err != nil {
		detail := r.Detail
		if detail == "" {
			detail = r.Err.Error()
		}
		m.enqueueToast(toastFail, r.Summary, firstLine(detail))
		return
	}
	m.enqueueToast(toastSuccess, r.Summary, "")
}

// renderToasts returns the right-aligned stack of toast banners to insert
// above the footer. Empty string when no toasts are active so the caller
// can skip the section entirely.
func (m *Model) renderToasts(width int) string {
	if len(m.toasts) == 0 {
		return ""
	}
	boxes := make([]string, 0, len(m.toasts))
	for _, t := range m.toasts {
		boxes = append(boxes, renderToastBox(t))
	}
	stack := strings.Join(boxes, "\n")
	// Right-align the stack within the full width so each toast sits in
	// the bottom-right corner of its row.
	return lipgloss.PlaceHorizontal(width, lipgloss.Right, stack)
}

// renderToastBox draws one toast: coloured severity dot, bold title, dim
// body (when present), and a dim " · d dismiss" hint. Wrapped in a thin
// border so it stands out from anything underneath. Width-capped so the
// title + dot doesn't expand to span the whole row.
func renderToastBox(t toast) string {
	dot := toastDot(t.kind)
	title := lipgloss.NewStyle().Bold(true).Render(t.title)
	body := t.body
	if body != "" {
		body = dimStyle.Render("  " + body)
	}
	hint := dimStyle.Render("  · d dismiss")

	line1 := dot + " " + title + hint
	contents := line1
	if body != "" {
		contents = line1 + "\n" + body
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(toastBorderColor(t.kind)).
		Padding(0, 1).
		MaxWidth(maxToastWidth)
	return style.Render(contents)
}

// toastDot returns the coloured glyph that identifies the toast's
// severity. Same palette as the running/failing/paused indicators so the
// visual language is consistent across panes.
func toastDot(k toastKind) string {
	switch k {
	case toastFail:
		return failingStyle.Render("●")
	case toastWarn:
		return pausedStyle.Render("●")
	default:
		return runningStyle.Render("●")
	}
}

func toastBorderColor(k toastKind) lipgloss.Color {
	switch k {
	case toastFail:
		return colFailing
	case toastWarn:
		return colPaused
	default:
		return colRunning
	}
}

// firstLineTitle abbreviates a summary when it would otherwise overflow
// the toast width. Currently unused but kept for the wider-summary case
// (e.g. multi-line CLI output). Truncates at maxToastWidth-8 to leave
// room for the dot + " · d dismiss" suffix.
func firstLineTitle(s string) string {
	if len(s) <= maxToastWidth-8 {
		return s
	}
	return fmt.Sprintf("%s…", s[:maxToastWidth-9])
}
