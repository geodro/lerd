// Package feedback renders lerd's CLI progress output (animated steps, a live
// line, summaries, prompts, warnings) and owns the shared lerd colour palette.
// It degrades to plain text when stdout is piped, redirected, or NO_COLOR is set.
package feedback

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// Canonical lerd palette (Tailwind hex values). The TUI theme aliases these so
// CLI feedback and the dashboard share one set of colours.
var (
	ColTitle   = lipgloss.Color("#FF2D20") // lerd red (Laravel brand)
	ColDim     = lipgloss.Color("#6b7280") // gray-500
	ColDivider = lipgloss.Color("#374151") // gray-700
	ColRunning = lipgloss.Color("#10b981") // emerald-500
	ColStopped = lipgloss.Color("#6b7280") // gray-500
	ColFailing = lipgloss.Color("#ef4444") // red-500
	ColPaused  = lipgloss.Color("#f59e0b") // amber-500
	ColGold    = lipgloss.Color("#fbbf24") // amber-400 (brighter gold for sudo prompts)
	// Interactive accent is the brand red so the TUI, CLI feedback, and the web
	// UI all share one accent rather than diverging on a separate hue.
	ColAccent = lipgloss.Color("#FF2D20") // lerd red
)

var (
	dimStyle    = lipgloss.NewStyle().Foreground(ColDim)
	okStyle     = lipgloss.NewStyle().Foreground(ColRunning)
	failStyle   = lipgloss.NewStyle().Foreground(ColFailing).Bold(true)
	valueStyle  = lipgloss.NewStyle().Foreground(ColAccent)
	promptStyle = lipgloss.NewStyle().Foreground(ColAccent).Bold(true)
	spinStyle   = lipgloss.NewStyle().Foreground(ColAccent)
	warnStyle   = lipgloss.NewStyle().Foreground(ColPaused)
	redStyle    = lipgloss.NewStyle().Foreground(ColFailing)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(ColTitle)
	goldStyle   = lipgloss.NewStyle().Bold(true).Foreground(ColGold)
)

// Status glyphs for query/report output that renders its own layout rather than
// using the Step/Live progress flow.
const (
	GlyphOK   = "✓"
	GlyphFail = "✗"
	GlyphWarn = "⚠"
	GlyphLock = "🔒"
)

// Title styles a fragment as the bold lerd-red wordmark colour.
func Title(s string) string { return paint(titleStyle, s) }

// Green, Red, Amber, and Dim colour a one-off fragment for status reports,
// honouring NO_COLOR and non-TTY output. Use these so query commands share the
// progress palette without reaching for raw ANSI codes.
func Green(s string) string { return paint(okStyle, s) }
func Red(s string) string   { return paint(redStyle, s) }
func Amber(s string) string { return paint(warnStyle, s) }
func Dim(s string) string   { return paint(dimStyle, s) }

// GreenIf, RedIf, and AmberIf colour s from the palette only when on is true,
// else return it plain — for renderers that target an arbitrary writer and gate
// colour per-writer (e.g. doctor forcing plain text into a bug report). When on
// is true the usual global NO_COLOR/TTY state is still honoured, so a coloured
// caller never produces escapes the environment asked to suppress.
func GreenIf(on bool, s string) string { return paintIf(on, okStyle, s) }
func RedIf(on bool, s string) string   { return paintIf(on, redStyle, s) }
func AmberIf(on bool, s string) string { return paintIf(on, warnStyle, s) }

// spinnerFrames is the Braille spinner used by the Live progress line.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// pad is the left margin printed before every feedback glyph line so the block
// sits slightly indented from the shell prompt.
const pad = " "

var (
	mu  sync.Mutex
	out io.Writer // nil → write to the current os.Stdout (so redirects are honoured)
	// colorOn is read on every paint() (including from the Live spinner
	// goroutine) and flipped by SetTestWriter, so it's atomic rather than a
	// plain bool guarded only on the write side.
	colorOn atomic.Bool
)

func init() { colorOn.Store(detectColor()) }

// reported collects errors already shown to the user by a Step/Live Fail. The
// top-level command handler consults it so a failure surfaced through the
// feedback UI isn't reprinted as a second raw "Error: …" line by cobra. It is
// capped because long-running processes (lerd-ui, the MCP server) also drive
// Fail through shared CLI helpers and would otherwise grow it without bound; a
// CLI command only ever checks the most recent failure, so an old-entry cap is
// harmless. The trim copies into a fresh slice so the backing array is freed.
const maxReported = 64

var (
	reportedMu sync.Mutex
	reported   []error
)

func markReported(err error) {
	if err == nil {
		return
	}
	reportedMu.Lock()
	reported = append(reported, err)
	if len(reported) > maxReported {
		reported = append([]error(nil), reported[len(reported)-maxReported:]...)
	}
	reportedMu.Unlock()
}

// AlreadyShown reports whether err, or any error it wraps, was already displayed
// to the user by a Fail. Callers in main use it to avoid double-printing.
func AlreadyShown(err error) bool {
	if err == nil {
		return false
	}
	reportedMu.Lock()
	defer reportedMu.Unlock()
	for _, r := range reported {
		if errors.Is(err, r) {
			return true
		}
	}
	return false
}

// target returns the writer to render to: an explicit test writer when set,
// otherwise the live os.Stdout (looked up per call so os.Stdout redirection,
// e.g. in tests, is respected).
func target() io.Writer {
	if out != nil {
		return out
	}
	return os.Stdout
}

func detectColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// colorEnabledFor reports whether coloured output should be written to w: only
// when NO_COLOR is unset and w is itself a terminal. Writer-targeted helpers
// (LineOn/WarnOn) gate on it so ANSI codes never leak into a captured buffer or
// a discarded/redirected writer, regardless of os.Stdout's TTY state.
func colorEnabledFor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// paint applies a style only when colour is enabled, so plain-mode output (and
// tests) stay free of escape codes.
func paint(st lipgloss.Style, s string) string {
	if !colorOn.Load() {
		return s
	}
	return st.Render(s)
}

// paintIf is paint gated by an explicit flag (and still honours global colour
// state), for callers that decide per-writer whether to colour.
func paintIf(on bool, st lipgloss.Style, s string) string {
	if !on {
		return s
	}
	return paint(st, s)
}

// Val styles a value fragment (violet) for embedding inside a step or summary.
func Val(s string) string { return paint(valueStyle, s) }

// Begin prints a single blank line to separate a feedback block from whatever
// (a shell prompt, a wizard) came before it.
func Begin() {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintln(target())
}

// Step is one line of progress: Start it before the work, then OK/Info/Fail
// finishes it. On a colour TTY a spinner animates until then; otherwise nothing
// prints until the finishing call writes the line once.
type Step struct {
	msg      string
	w        io.Writer // fixed render target; nil → the live target() (honours redirects)
	animated bool      // snapshotted at Start so finish() can't disagree with start()
	stop     chan struct{}
	wg       sync.WaitGroup
	prev     *activeBox // active spinner this one suspends, restored on finish
	paused   bool       // guarded by the package mu; set by Interrupt
}

// Start records a step with the given present-tense label (e.g. "detecting
// framework") and begins its spinner on an animated terminal.
func Start(msg string) *Step { return start(msg, nil) }

// StartOn is Start with a fixed render target. The spinner and finishing line
// write to w rather than the live os.Stdout, so the step can animate while the
// caller redirects os.Stdout elsewhere (e.g. capturing a sub-step's output)
// without the spinner frames leaking into that redirect.
func StartOn(w io.Writer, msg string) *Step { return start(msg, w) }

func start(msg string, w io.Writer) *Step {
	s := &Step{msg: msg, w: w, animated: Animated()}
	if s.animated {
		s.stop = make(chan struct{})
		s.prev = pushActive(s)
		s.wg.Add(1)
		go s.spin()
	}
	return s
}

// dst is the step's render target: its fixed writer when set, else the live one.
func (s *Step) dst() io.Writer {
	if s.w != nil {
		return s.w
	}
	return target()
}

func (s *Step) spin() {
	defer s.wg.Done()
	t := time.NewTicker(90 * time.Millisecond)
	defer t.Stop()
	i := 0
	s.frame(spinnerFrames[0])
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			i++
			s.frame(spinnerFrames[i%len(spinnerFrames)])
		}
	}
}

func (s *Step) frame(f string) {
	mu.Lock()
	defer mu.Unlock()
	if s.paused {
		return
	}
	fmt.Fprintf(s.dst(), "\r\033[2K%s%s %s %s", pad, paint(dimStyle, "→"), paint(dimStyle, s.msg), paint(spinStyle, f))
}

// Interrupt suspends the step's spinner and clears its line so fn can print
// standalone output above it, mirroring Live.Interrupt. On a non-animated step
// it just runs fn. paused is read and written under the package mu, the same
// lock frame() holds, so no redraw slips between the clear and fn's output.
func (s *Step) Interrupt(fn func()) {
	if !s.animated {
		fn()
		return
	}
	mu.Lock()
	wasPaused := s.paused
	s.paused = true
	if !wasPaused {
		fmt.Fprint(s.dst(), "\r\033[2K")
	}
	mu.Unlock()
	fn()
	mu.Lock()
	s.paused = wasPaused
	mu.Unlock()
}

func (s *Step) finish(mark, result string) {
	if s.animated {
		close(s.stop)
		s.wg.Wait()
		popActive(s.prev)
	}
	mu.Lock()
	defer mu.Unlock()
	line := fmt.Sprintf("%s%s %s", pad, paint(dimStyle, "→"), paint(dimStyle, s.msg+"…"))
	if mark != "" {
		line += " " + mark
	}
	if result != "" {
		line += " " + result
	}
	if s.animated {
		fmt.Fprintf(s.dst(), "\r\033[2K%s\n", line)
	} else {
		fmt.Fprintln(s.dst(), line)
	}
}

// OK collapses the step with a green check and a result (e.g. "Laravel 11").
func (s *Step) OK(result string) { s.finish(paint(okStyle, "✓"), result) }

// Info collapses the step with a plain result and no mark.
func (s *Step) Info(result string) { s.finish("", result) }

// failText is the message rendered next to a ✗. A cross must never appear
// without a reason, so a nil or empty-string error falls back to a placeholder
// instead of leaving the user staring at a bare red mark.
func failText(err error) string {
	if err != nil {
		if msg := err.Error(); msg != "" {
			return msg
		}
	}
	return "failed (no error detail reported)"
}

// Fail collapses the step with a red cross and the error text.
func (s *Step) Fail(err error) {
	markReported(err)
	s.finish(paint(failStyle, "✗"), paint(failStyle, failText(err)))
}

// Warn collapses the step with an amber warning glyph and message, for a
// non-fatal hiccup (e.g. a best-effort restart that didn't take) that should be
// noticed but doesn't mean the command itself failed. Unlike Fail it draws no
// red cross, so it doesn't read as the whole operation having errored.
func (s *Step) Warn(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.finish(paint(warnStyle, GlyphWarn), paint(warnStyle, msg))
}

// Fail prints a standalone red ✗ line for err to stderr in the same style (and
// blank-line spacing) as a Live/Step failure. It is for the top-level command
// handler to render an error a command returned without surfacing it itself, so
// every failure reads the same whether it came up through a spinner or bubbled
// out raw. Errors go to stderr so stdout stays clean for piped data. It records
// err as shown so nothing reprints it.
func Fail(err error) { FailOn(os.Stderr, err) }

// FailOn is Fail targeted at an explicit writer, colouring only when that writer
// is a terminal. Lets the top-level handler send errors to stderr and tests
// capture them in a buffer.
func FailOn(w io.Writer, err error) {
	if err == nil {
		return
	}
	markReported(err)
	on := colorEnabledFor(w)
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(w, "\n%s%s %s\n\n", pad, paintIf(on, failStyle, GlyphFail), paintIf(on, failStyle, failText(err)))
}

// Line prints a standalone step (arrow prefix, no trailing ellipsis) for an
// already-known fact, e.g. "php 8.4 · node 22 · nginx vhost written".
func Line(msg string) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(target(), "%s%s %s\n", pad, paint(dimStyle, "→"), msg)
}

// LineOn is Line targeted at an explicit writer — for an install step that
// captures or discards its sub-output (e.g. via io.Discard) rather than writing
// to the live stdout. Mirrors Line's styling so captured output reads the same.
func LineOn(w io.Writer, msg string) {
	on := colorEnabledFor(w)
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(w, "%s%s %s\n", pad, paintIf(on, dimStyle, "→"), msg)
}

// Header prints a section header — a brand-red "▸ title" with a blank line
// above and below — to delimit the major phases of a long multi-step command
// (install, update) so the step lines beneath read as a group instead of one
// flat wall of output. Routed through emit so it lands cleanly above any live
// spinner that is still animating.
func Header(title string) {
	emit(func() {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(target(), "\n%s%s %s\n\n", pad, paint(titleStyle, "▸"), paint(titleStyle, title))
	})
}

// Note prints a dim, indented sub-detail under a step (e.g. a log hint). It has
// no glyph so it reads as secondary to the step lines above it.
func Note(msg string) {
	emit(func() {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(target(), "%s  %s\n", pad, paint(dimStyle, msg))
	})
}

// Sudo prints a privileged-step line: a gold lock glyph and gold message, so
// the password prompt the next command (mkcert, a sudo write) raises reads as
// expected rather than a surprise. Use it in place of a bare "[sudo required] …"
// print so every privileged step looks the same. Routed through emit so it
// lands cleanly above any live spinner.
func Sudo(msg string) {
	emit(func() {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(target(), "%s%s %s\n", pad, paint(goldStyle, GlyphLock), paint(goldStyle, msg))
	})
}

// Warn prints a warning line: an amber warning glyph and amber message. Use in
// place of a plain "[WARN] …" print.
func Warn(format string, a ...any) {
	emit(func() {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(target(), "%s%s %s\n", pad, paint(warnStyle, "⚠"), paint(warnStyle, fmt.Sprintf(format, a...)))
	})
}

// WarnOn is Warn targeted at an explicit writer rather than the live stdout,
// for callers that capture or discard their sub-output. Mirrors Warn's styling.
func WarnOn(w io.Writer, format string, a ...any) {
	on := colorEnabledFor(w)
	msg := fmt.Sprintf(format, a...)
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(w, "%s%s %s\n", pad, paintIf(on, warnStyle, "⚠"), paintIf(on, warnStyle, msg))
}

// Table prints headers + rows as a single aligned table in the lerd palette so
// every `lerd … list`/`search` view shares one look instead of hand-rolled
// printf columns. See RenderTable for the styling and plain-mode fallback.
func Table(headers []string, rows [][]string) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintln(target(), RenderTable(headers, rows))
}

// tableWidth reports the column budget a rendered table must fit within: the
// terminal width, or 0 when it can't be determined (piped/redirected) so the
// table renders at its natural width. A package var so tests can pin a width.
var tableWidth = func() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 0
}

// RenderTable returns the table as a string without printing it, for callers
// that compose it into other output. On a colour TTY it draws a rounded grid
// with a bold brand-red header and dim borders; with colour off (NO_COLOR or a
// pipe) it falls back to plain space-aligned columns so scripted output stays
// parseable. When the natural grid is wider than the terminal the widest
// columns are truncated with an ellipsis so every row stays on one line and the
// grid fits the screen whenever the column count leaves room for it.
func RenderTable(headers []string, rows [][]string) string {
	if !colorOn.Load() {
		return plainTable(headers, rows)
	}
	if w := tableWidth(); w > 0 {
		headers, rows = fitColumns(headers, rows, w)
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(ColTitle).Padding(0, 1)
	cell := lipgloss.NewStyle().Padding(0, 1)
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ColDivider)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return header
			}
			return cell
		})
	return t.String()
}

// fitColumns shrinks a table to fit budget columns wide by repeatedly trimming
// the widest column and ellipsis-truncating its cells, so rows stay single-line.
// Rounded borders cost one cell per separator (N+1) plus two padding cells per
// column, so the content budget is budget-(3N+1). Truncation is ANSI-aware so
// pre-coloured cells keep their styling.
func fitColumns(headers []string, rows [][]string, budget int) ([]string, [][]string) {
	n := len(headers)
	if n == 0 {
		return headers, rows
	}
	widths := make([]int, n)
	for i, h := range headers {
		widths[i] = ansi.StringWidth(h)
	}
	for _, r := range rows {
		for i := 0; i < n && i < len(r); i++ {
			if w := ansi.StringWidth(r[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	total := 0
	for _, w := range widths {
		total += w
	}
	avail := budget - (3*n + 1)
	if total <= avail {
		return headers, rows // already fits
	}
	// On a terminal too narrow even for the per-column floor, shrink everything
	// to the floor anyway: the grid may still exceed the screen, but clamped to
	// the minimum it can be rather than spilling at full natural width.
	const minCol = 6

	for total > avail {
		maxI, maxW := -1, minCol
		for i, w := range widths {
			if w > maxW {
				maxI, maxW = i, w
			}
		}
		if maxI < 0 {
			break // every column already at the floor
		}
		widths[maxI]--
		total--
	}

	outHeaders := make([]string, n)
	for i, h := range headers {
		outHeaders[i] = ansi.Truncate(h, widths[i], "…")
	}
	outRows := make([][]string, len(rows))
	for ri, r := range rows {
		nr := make([]string, len(r))
		copy(nr, r)
		for i := 0; i < n && i < len(nr); i++ {
			nr[i] = ansi.Truncate(nr[i], widths[i], "…")
		}
		outRows[ri] = nr
	}
	return outHeaders, outRows
}

// plainTable lays the same data out as space-aligned columns with a two-space
// gutter and no borders or colour, for piped/NO_COLOR output. Widths are
// measured with lipgloss.Width so any pre-styled cells still align.
func plainTable(headers []string, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, r := range rows {
		for i := 0; i < len(widths) && i < len(r); i++ {
			if w := lipgloss.Width(r[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}
	var b strings.Builder
	writeRow := func(cells []string) {
		for i := range widths {
			val := ""
			if i < len(cells) {
				val = cells[i]
			}
			b.WriteString(val)
			if i < len(widths)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(val)+2))
			}
		}
		b.WriteString("\n")
	}
	writeRow(headers)
	for _, r := range rows {
		writeRow(r)
	}
	return strings.TrimRight(b.String(), "\n")
}

// interruptible is anything that animates a spinner line and can pause it so a
// standalone print lands cleanly above it. Both Step and Live implement it.
type interruptible interface{ Interrupt(fn func()) }

// activeBox boxes the active interruptible so it can live in an atomic.Pointer
// (which needs a concrete element type).
type activeBox struct{ line interruptible }

// liveActive holds the spinner currently animating, if any, so standalone
// prints (Warn / Note) can pause it and print cleanly above the live line
// instead of being clobbered by its in-place redraw.
var liveActive atomic.Pointer[activeBox]

// pushActive registers l as the active spinner, returning the one it suspends
// (restored later via popActive).
func pushActive(l interruptible) *activeBox { return liveActive.Swap(&activeBox{line: l}) }

// popActive restores the previously-active spinner saved by pushActive.
func popActive(prev *activeBox) { liveActive.Store(prev) }

// emit runs a standalone print, routing it through the active spinner's
// Interrupt when one is animating so it doesn't overwrite it. With no active
// spinner it just runs fn.
func emit(fn func()) {
	if b := liveActive.Load(); b != nil {
		b.line.Interrupt(fn)
		return
	}
	fn()
}

// Done prints a green-check completion line with no timing, for operations
// where elapsed time isn't meaningful.
func Done(msg string) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(target(), "%s%s %s\n", pad, paint(okStyle, "✓"), msg)
}

// Success prints the terminal line: green check, message, dim elapsed time.
func Success(msg string, d time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(target(), "%s%s %s %s\n", pad, paint(okStyle, "✓"), msg, paint(dimStyle, "in "+humanDur(d)))
}

// Confirm prints a styled yes/no prompt (preceded by a blank line) and reads
// the answer from stdin, returning defaultYes on an empty response. The prompt
// matches the step styling: a violet "?" lead-in and a dim "[Y/n]" hint.
func Confirm(question string, defaultYes bool) bool {
	hint := "[Y/n]"
	if !defaultYes {
		hint = "[y/N]"
	}
	mu.Lock()
	fmt.Fprintf(target(), "\n%s%s %s %s ", pad, paint(promptStyle, "?"), question, paint(dimStyle, hint))
	mu.Unlock()

	var answer string
	fmt.Scanln(&answer) //nolint:errcheck
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes
	}
	return answer[0] == 'y'
}

// Prompt renders a styled yes/no question (violet "?", dim "[Y/n]" hint),
// preceded by a blank line, WITHOUT reading the answer — for callers that read
// from a custom source (e.g. /dev/tty so `curl … | bash` prompts still work)
// instead of stdin. Mirrors Confirm's styling so every prompt looks the same.
func Prompt(question string, defaultYes bool) {
	hint := "[Y/n]"
	if !defaultYes {
		hint = "[y/N]"
	}
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintf(target(), "\n%s%s %s %s ", pad, paint(promptStyle, "?"), question, paint(dimStyle, hint))
}

// Animated reports whether output supports in-place animation (a colour TTY).
// When false, Live degrades to a header line plus one plain line per item.
func Animated() bool { return colorOn.Load() }

// interactiveFn is the swappable backing for Interactive so tests can pin it
// via SetInteractive without exposing a mutable public symbol.
var interactiveFn = func() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// Interactive reports whether stdout is a terminal, independent of colour. Use
// it to choose a condensed single-line view over full verbose output: a user
// who only disabled colour (NO_COLOR) still gets the interactive view rather
// than the pipe/MCP verbose path. Animated() is the wrong gate for that, since
// NO_COLOR forces it false even on a real terminal.
func Interactive() bool { return interactiveFn() }

// SetInteractive overrides Interactive for a test and returns a restore func.
func SetInteractive(on bool) func() {
	prev := interactiveFn
	interactiveFn = func() bool { return on }
	return func() { interactiveFn = prev }
}

// Live is a single in-place progress line with a trailing spinner that
// accumulates completed items, e.g. "→ configuring .env… ✓ a · b · c ⠙". When
// it finishes the spinner is dropped and the ✓ line stays put.
type Live struct {
	msg      string
	imu      sync.Mutex
	items    []string
	animated bool       // snapshotted at StartLive so Done/Fail can't disagree
	paused   bool       // guarded by the package mu; set by Interrupt
	prev     *activeBox // the spinner this one suspends, restored on Done/Fail
	stop     chan struct{}
	wg       sync.WaitGroup
}

// StartLive begins a live line with the given label. On a non-animated output
// it just prints the header and each Add on its own line.
func StartLive(msg string) *Live {
	l := &Live{msg: msg, animated: Animated()}
	if l.animated {
		// Register as the active line (saving any line we nest inside) so
		// Warn/Note route through Interrupt while this spinner animates. Only
		// the animated spinner clobbers standalone prints, so plain mode skips
		// this and prints warnings on their own line as before.
		l.prev = pushActive(l)
		l.stop = make(chan struct{})
		l.wg.Add(1)
		go l.spin()
	} else {
		mu.Lock()
		fmt.Fprintf(target(), "%s%s %s\n", pad, "→", msg+"…")
		mu.Unlock()
	}
	return l
}

func (l *Live) snapshot() []string {
	l.imu.Lock()
	defer l.imu.Unlock()
	return append([]string(nil), l.items...)
}

func (l *Live) spin() {
	defer l.wg.Done()
	t := time.NewTicker(90 * time.Millisecond)
	defer t.Stop()
	i := 0
	l.draw(spinnerFrames[0])
	for {
		select {
		case <-l.stop:
			return
		case <-t.C:
			i++
			l.draw(spinnerFrames[i%len(spinnerFrames)])
		}
	}
}

func (l *Live) draw(frame string) {
	items := l.snapshot()
	mu.Lock()
	defer mu.Unlock()
	if l.paused {
		return
	}
	line := pad + paint(dimStyle, "→") + " " + paint(dimStyle, l.msg+"…")
	if len(items) > 0 {
		line += " " + paint(okStyle, "✓") + " " + strings.Join(items, " · ")
	}
	if frame != "" {
		line += " " + paint(spinStyle, frame)
	}
	fmt.Fprintf(target(), "\r\033[2K%s", line)
}

// Interrupt suspends the spinner and clears its line so fn can print standalone
// output (a booting service, a child process's stdout) above the live line
// without the spinner's \r redraw clobbering it, then lets the spinner resume
// and redraw beneath. On non-animated output it just runs fn. paused is read
// and written under the package mu, the same lock draw holds while writing, so
// no redraw can slip between the clear and fn's output.
func (l *Live) Interrupt(fn func()) {
	if !l.animated {
		fn()
		return
	}
	mu.Lock()
	// Save and restore the prior paused state so nested Interrupts (e.g. a
	// child step that itself emits a Warn) keep the line paused until the
	// outermost call returns, rather than resuming the spinner early.
	wasPaused := l.paused
	l.paused = true
	if !wasPaused {
		fmt.Fprint(target(), "\r\033[2K")
	}
	mu.Unlock()
	fn()
	mu.Lock()
	l.paused = wasPaused
	mu.Unlock()
}

// Add records a completed item; the spinning line redraws to include it.
func (l *Live) Add(item string) {
	if !l.animated {
		mu.Lock()
		fmt.Fprintf(target(), "%s  %s\n", pad, item)
		mu.Unlock()
		return
	}
	l.imu.Lock()
	l.items = append(l.items, item)
	l.imu.Unlock()
}

// Done stops the spinner, leaving the ✓ line (checkbox replaces the loader).
func (l *Live) Done() {
	if !l.animated {
		mu.Lock()
		fmt.Fprintf(target(), "%s%s %s\n", pad, paint(okStyle, "✓"), l.msg)
		mu.Unlock()
		return
	}
	// Stop the spinner before deregistering, so a concurrent Warn never routes
	// past this line while the goroutine is still drawing it.
	close(l.stop)
	l.wg.Wait()
	popActive(l.prev)
	l.draw("")
	mu.Lock()
	fmt.Fprintln(target())
	mu.Unlock()
}

// Fail stops the spinner and finalises the line with a red cross.
func (l *Live) Fail(err error) {
	markReported(err)
	if l.animated {
		// Stop the spinner before deregistering (see Done).
		close(l.stop)
		l.wg.Wait()
		popActive(l.prev)
	}
	mu.Lock()
	defer mu.Unlock()
	if l.animated {
		fmt.Fprint(target(), "\r\033[2K")
	}
	msg := failText(err)
	// A live line fails the whole operation, so give the cross some breathing
	// room with a blank line above and below, matching the top-level handler.
	fmt.Fprintf(target(), "\n%s%s %s\n\n", pad, paint(failStyle, "✗"), paint(failStyle, msg))
}

// Summary is an aligned key/value block printed after an operation completes.
type Summary struct{ rows [][2]string }

// NewSummary returns an empty summary block.
func NewSummary() *Summary { return &Summary{} }

// Row appends a label/value pair; value may contain styled fragments via Val.
func (s *Summary) Row(label, value string) *Summary {
	s.rows = append(s.rows, [2]string{label, value})
	return s
}

// Print renders the block, label column padded to the widest label, preceded by
// a blank line. No-op when empty.
func (s *Summary) Print() {
	if len(s.rows) == 0 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	w := 0
	for _, r := range s.rows {
		if len(r[0]) > w {
			w = len(r[0])
		}
	}
	fmt.Fprintln(target())
	for _, r := range s.rows {
		label := r[0] + strings.Repeat(" ", w-len(r[0]))
		fmt.Fprintf(target(), "  %s   %s\n", paint(dimStyle, label), r[1])
	}
}

// SetTestWriter redirects output to w in plain mode (no colour) and returns a
// restore func. Intended for tests in this and other packages.
func SetTestWriter(w io.Writer) func() {
	mu.Lock()
	prevOut, prevColor := out, colorOn.Load()
	out = w
	colorOn.Store(false)
	mu.Unlock()
	return func() {
		mu.Lock()
		out = prevOut
		colorOn.Store(prevColor)
		mu.Unlock()
	}
}

// SetAnimated forces Animated() to on (or off) and returns a restore func.
// Pair it with SetTestWriter so a test can exercise an animated code path while
// the spinner frames land in a buffer instead of os.Stdout.
func SetAnimated(on bool) func() {
	prev := colorOn.Load()
	colorOn.Store(on)
	return func() { colorOn.Store(prev) }
}

func humanDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
