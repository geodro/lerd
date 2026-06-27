package feedback

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestStepOKPlain(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	Start("detecting framework").OK("Laravel 11")

	got := buf.String()
	want := " → detecting framework… ✓ Laravel 11\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// StartOn renders to its own writer so a step can animate on the real stdout
// while the caller redirects the package target elsewhere (e.g. to capture a
// sub-step's output), without the step line leaking into that redirect.
func TestStartOnWritesToGivenWriter(t *testing.T) {
	var pkg, fixed bytes.Buffer
	defer SetTestWriter(&pkg)()

	StartOn(&fixed, "installing deps").OK("ok")

	if pkg.Len() != 0 {
		t.Errorf("StartOn wrote to the package target, want only the fixed writer: %q", pkg.String())
	}
	if got := fixed.String(); got != " → installing deps… ✓ ok\n" {
		t.Errorf("StartOn output = %q", got)
	}
}

func TestStepInfoAndFail(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	Start("writing vhost").Info("done")
	Start("provisioning TLS").Fail(errors.New("mkcert missing"))

	got := buf.String()
	if !strings.Contains(got, " → writing vhost… done\n") {
		t.Errorf("missing info line: %q", got)
	}
	if !strings.Contains(got, " → provisioning TLS… ✗ mkcert missing\n") {
		t.Errorf("missing fail line: %q", got)
	}
}

// Warn collapses a step with the amber ⚠ glyph and no red cross, and (unlike
// Fail) does not mark its error as already-shown, since it is a non-fatal hiccup
// rather than a failure the command is returning.
func TestStepWarn(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	warnErr := errors.New("unit not loaded")
	Start("restarting lerd-ui").Warn(warnErr)

	got := buf.String()
	if !strings.Contains(got, " → restarting lerd-ui… ⚠ unit not loaded\n") {
		t.Errorf("missing warn line: %q", got)
	}
	if strings.Contains(got, "✗") {
		t.Errorf("warn must not render a red cross: %q", got)
	}
	if AlreadyShown(warnErr) {
		t.Error("Warn should not mark its error as already-shown")
	}
}

// A Step/Live Fail records its error so the top-level handler can tell it was
// already shown to the user, covering both the bare error returned as-is and a
// wrapped error that adds context around it. An unrelated error stays unshown.
func TestAlreadyShownAfterFail(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	bare := errors.New("mkcert missing")
	Start("provisioning TLS").Fail(bare)

	if !AlreadyShown(bare) {
		t.Error("bare error not marked AlreadyShown after Fail")
	}
	wrapped := fmt.Errorf("provisioning TLS: %w", bare)
	if !AlreadyShown(wrapped) {
		t.Error("error wrapping a shown error not detected as AlreadyShown")
	}
	if AlreadyShown(errors.New("something else")) {
		t.Error("unrelated error wrongly reported as AlreadyShown")
	}
	if AlreadyShown(nil) {
		t.Error("nil error reported as AlreadyShown")
	}
}

// Header frames a section with a ▸ glyph and a blank line above and below, so
// the steps beneath it read as a group rather than a flat wall of output.
// The *If helpers gate colour on an explicit flag: on=false always returns the
// plain string (so a renderer writing a plain-text bug report gets no escapes),
// while on=true delegates to the same palette painter as the unconditional form.
func TestColourIf(t *testing.T) {
	if got := GreenIf(false, "ok"); got != "ok" {
		t.Errorf("GreenIf(false) = %q, want plain", got)
	}
	if got := RedIf(false, "bad"); got != "bad" {
		t.Errorf("RedIf(false) = %q, want plain", got)
	}
	if got := AmberIf(false, "warn"); got != "warn" {
		t.Errorf("AmberIf(false) = %q, want plain", got)
	}
	if GreenIf(true, "ok") != Green("ok") {
		t.Error("GreenIf(true) should match Green()")
	}
	if RedIf(true, "bad") != Red("bad") {
		t.Error("RedIf(true) should match Red()")
	}
}

func TestHeader(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	Header("Rebuilding PHP images")

	if got := buf.String(); got != "\n ▸ Rebuilding PHP images\n\n" {
		t.Fatalf("Header output = %q", got)
	}
}

// Prompt renders the same styled question as Confirm (blank line, "?", dim
// hint) but reads nothing, for callers that read from their own source.
func TestPrompt(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	Prompt("Continue?", true)
	if got := buf.String(); got != "\n ? Continue? [Y/n] " {
		t.Fatalf("Prompt(Y) = %q", got)
	}
	buf.Reset()
	Prompt("Wipe data?", false)
	if got := buf.String(); got != "\n ? Wipe data? [y/N] " {
		t.Fatalf("Prompt(N) = %q", got)
	}
}

func TestLine(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	Line("php 8.4 · node 22 · nginx vhost written")

	if got := buf.String(); got != " → php 8.4 · node 22 · nginx vhost written\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSuccess(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	Success("linked", 1800*time.Millisecond)

	if got := buf.String(); got != " ✓ linked in 1.8s\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDone(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	Done("unparked sites")

	if got := buf.String(); got != " ✓ unparked sites\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSummaryAligned(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	NewSummary().
		Row("Site", "https://acme.test").
		Row("PHP", "8.4.3 · FPM running").
		Row("DB", "mysql · cache redis").
		Print()

	want := "\n" +
		"  Site   https://acme.test\n" +
		"  PHP    8.4.3 · FPM running\n" +
		"  DB     mysql · cache redis\n"
	if got := buf.String(); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestEmptySummaryPrintsNothing(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	NewSummary().Print()

	if got := buf.String(); got != "" {
		t.Fatalf("expected no output, got %q", got)
	}
}

func TestValPlainPassthrough(t *testing.T) {
	defer SetTestWriter(&bytes.Buffer{})()
	if got := Val("8.4"); got != "8.4" {
		t.Fatalf("plain Val should not style, got %q", got)
	}
}

func TestHumanDur(t *testing.T) {
	cases := map[time.Duration]string{
		500 * time.Millisecond:  "500ms",
		1800 * time.Millisecond: "1.8s",
		2 * time.Second:         "2.0s",
	}
	for d, want := range cases {
		if got := humanDur(d); got != want {
			t.Errorf("humanDur(%v) = %q, want %q", d, got, want)
		}
	}
}

// withAnimatedBuffer forces animated mode against buf so the Live spinner path
// runs. SetTestWriter forces plain mode (which short-circuits Interrupt), so we
// set the package globals directly and restore them on cleanup.
func withAnimatedBuffer(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	mu.Lock()
	prevOut, prevColor := out, colorOn.Load()
	out = buf
	colorOn.Store(true)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		out = prevOut
		colorOn.Store(prevColor)
		mu.Unlock()
	})
}

func TestLiveInterrupt_SuppressesSpinnerWhilePaused(t *testing.T) {
	var buf bytes.Buffer
	withAnimatedBuffer(t, &buf)

	l := &Live{msg: "configuring .env", animated: true}
	buf.Reset()

	// A spinner tick that fires during the interrupt must not redraw, and the
	// callback's own output must reach the writer.
	l.Interrupt(func() {
		l.draw("⠙") // simulate the spinner goroutine racing the pause
		fmt.Fprint(&buf, "  Starting mysql...\n")
	})

	got := buf.String()
	if strings.Contains(got, "configuring .env") {
		t.Fatalf("spinner redrew while paused: %q", got)
	}
	if !strings.Contains(got, "Starting mysql...") {
		t.Fatalf("interrupt callback output missing: %q", got)
	}

	// After the interrupt the spinner resumes and draws normally again.
	buf.Reset()
	l.draw("⠹")
	if !strings.Contains(buf.String(), "configuring .env") {
		t.Fatalf("spinner did not resume after interrupt: %q", buf.String())
	}
}

func TestLiveInterrupt_PlainModeJustRunsFn(t *testing.T) {
	var buf bytes.Buffer
	defer SetTestWriter(&buf)()

	l := StartLive("configuring .env")
	ran := false
	l.Interrupt(func() { ran = true })
	if !ran {
		t.Fatal("Interrupt did not run fn in plain mode")
	}
}

// TestWarn_PausesActiveLine verifies that a Warn emitted while a live spinner is
// animating is routed through Interrupt: the spinner doesn't clobber it and the
// warning text reaches the writer. This is the env.go clobber regression.
func TestWarn_PausesActiveLine(t *testing.T) {
	var buf bytes.Buffer
	withAnimatedBuffer(t, &buf)

	l := &Live{msg: "configuring .env", animated: true}
	prev := pushActive(l)
	t.Cleanup(func() { popActive(prev) })

	buf.Reset()
	Warn("could not start %s: boom", "mysql")

	got := buf.String()
	if !strings.Contains(got, "could not start mysql") {
		t.Fatalf("warning text missing: %q", got)
	}
	// The line must have been cleared (Interrupt's \r\033[2K) so the spinner
	// redraw doesn't overwrite the warning.
	if !strings.Contains(got, "\r\033[2K") {
		t.Fatalf("warn did not pause/clear the live line: %q", got)
	}
}

// TestStartLive_TracksActiveAcrossNesting verifies StartLive registers the active
// line and Done/Fail restore the previous one, so a nested configure step doesn't
// leave the outer line unregistered (which would re-expose the clobber).
func TestStartLive_TracksActiveAcrossNesting(t *testing.T) {
	var buf bytes.Buffer
	withAnimatedBuffer(t, &buf)

	baseline := liveActive.Load()
	t.Cleanup(func() { liveActive.Store(baseline) })

	activeLine := func() interruptible {
		if b := liveActive.Load(); b != nil {
			return b.line
		}
		return nil
	}

	outer := StartLive("outer")
	if activeLine() != outer {
		t.Fatal("StartLive did not register the outer line")
	}
	inner := StartLive("inner")
	if activeLine() != inner {
		t.Fatal("nested StartLive did not register the inner line")
	}
	inner.Done()
	if activeLine() != outer {
		t.Fatal("inner Done did not restore the outer line")
	}
	outer.Fail(nil)
	if liveActive.Load() != baseline {
		t.Fatal("outer Fail did not restore the baseline active line")
	}
}

func TestFailTextNeverEmpty(t *testing.T) {
	if got := failText(nil); got == "" {
		t.Error("failText(nil) must not be empty — a ✗ needs a reason")
	}
	if got := failText(errors.New("")); got == "" {
		t.Error("failText of an empty-message error must not be empty")
	}
	if got := failText(errors.New("boom")); got != "boom" {
		t.Errorf("failText = %q, want %q", got, "boom")
	}
}

// End-to-end: every ✗-rendering path must route through failText so a cross is
// never drawn without a reason, even when handed a nil or empty error.
func TestFailPathsAlwaysShowReason(t *testing.T) {
	render := func(run func()) string {
		var buf bytes.Buffer
		restore := SetTestWriter(&buf)
		run()
		restore()
		return buf.String()
	}
	cases := map[string]func(){
		"Step.Fail(nil)":      func() { Start("starting lerd-dns").Fail(nil) },
		"Step.Fail(emptyErr)": func() { Start("starting lerd-redis").Fail(errors.New("")) },
		"Live.Fail(nil)":      func() { StartLive("pulling image").Fail(nil) },
	}
	for name, run := range cases {
		out := render(run)
		if !strings.Contains(out, "✗") || !strings.Contains(out, "no error detail") {
			t.Errorf("%s drew a ✗ without a reason: %q", name, out)
		}
	}
}
