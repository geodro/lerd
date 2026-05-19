package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dumps"
)

func TestNotificationForDump_Shape(t *testing.T) {
	evt := dumps.Event{ID: "abc", Kind: "dump", Ctx: dumps.Context{Site: "astrolov.test", Type: "fpm"}}
	n := notificationForDump(evt)
	if n.Kind != "dump" {
		t.Errorf("Kind = %q", n.Kind)
	}
	if n.Params["site"] != "astrolov.test" {
		t.Errorf("Params.site = %q", n.Params["site"])
	}
	if n.Params["kind"] != "fpm" {
		t.Errorf("Params.kind = %q", n.Params["kind"])
	}
	// No site is registered with that name in this test, so siteDomainForRoute
	// falls back to the input verbatim. The URL still lands on a sites sub-tab
	// route shape the frontend can parse.
	if n.URL != "#sites/astrolov.test/dumps" {
		t.Errorf("URL = %q", n.URL)
	}
}

func TestNotificationForDump_BodyContainsDumpText(t *testing.T) {
	evt := dumps.Event{
		ID:   "abc",
		Kind: "dump",
		Ctx:  dumps.Context{Site: "astrolov.test", Type: "fpm"},
		Text: "string(5) \"hello\"",
	}
	n := notificationForDump(evt)
	if n.Body != "string(5) \"hello\"" {
		t.Errorf("Body = %q, want dump text passed through", n.Body)
	}
	if n.Params["text"] != "string(5) \"hello\"" {
		t.Errorf("Params.text = %q", n.Params["text"])
	}
}

func TestNotificationForDump_TextTruncatedAndSingleLine(t *testing.T) {
	long := "line1\nline2  with   extra spaces\n" + string(make([]byte, 300))
	evt := dumps.Event{
		ID:   "abc",
		Kind: "dump",
		Ctx:  dumps.Context{Site: "x", Type: "fpm"},
		Text: long,
	}
	n := notificationForDump(evt)
	if len(n.Body) > 160 {
		t.Errorf("Body too long: %d chars", len(n.Body))
	}
	for _, c := range n.Body {
		if c == '\n' || c == '\r' {
			t.Errorf("Body contains newlines: %q", n.Body)
			break
		}
	}
}

// Truncating a multi-byte rune mid-byte produced � replacement chars
// in the notification body. Build a text whose 139-byte cut would split a
// rune and assert no replacement chars sneak in.
func TestDumpPreview_UTF8BoundarySafe(t *testing.T) {
	// 47 × 3-byte rune = 141 bytes — first 139 bytes lands mid-rune.
	text := strings.Repeat("☃", 47)
	got := dumpPreview(text)
	if strings.ContainsRune(got, '�') {
		t.Errorf("preview contains U+FFFD replacement char: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("preview should end with ellipsis: %q", got)
	}
}

func TestNotificationForDump_EmptyTextFallsBack(t *testing.T) {
	evt := dumps.Event{ID: "abc", Kind: "dump", Ctx: dumps.Context{Site: "x", Type: "fpm"}}
	n := notificationForDump(evt)
	if n.Body == "" {
		t.Error("Body should fall back to a description, not be empty")
	}
}

func TestDumpDebouncer_FirstEventPasses(t *testing.T) {
	d := newDumpDebouncer(time.Second)
	if !d.allow("a.test") {
		t.Error("first event for site should pass")
	}
}

func TestDumpDebouncer_SecondEventWithinWindowBlocked(t *testing.T) {
	d := newDumpDebouncer(time.Second)
	d.allow("a.test")
	if d.allow("a.test") {
		t.Error("second event within debounce window should be blocked")
	}
}

func TestDumpDebouncer_SecondEventAfterWindowPasses(t *testing.T) {
	d := newDumpDebouncer(10 * time.Millisecond)
	d.allow("a.test")
	time.Sleep(20 * time.Millisecond)
	if !d.allow("a.test") {
		t.Error("event after window should pass")
	}
}

func TestDumpDebouncer_DifferentSitesIndependent(t *testing.T) {
	d := newDumpDebouncer(time.Hour)
	if !d.allow("a.test") {
		t.Error("a.test first should pass")
	}
	if !d.allow("b.test") {
		t.Error("b.test should pass independently of a.test")
	}
}

// The dump-bridge tags Ctx.Site with the registered site name (the value
// of LERD_SITE), but the dashboard router keys the Sites tab by primary
// domain. notificationForDump must resolve name → primary domain so the
// click handler lands on the right site detail.
func TestNotificationForDump_URLResolvesNameToDomain(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := config.AddSite(config.Site{
		Name:    "whitewaters",
		Domains: []string{"theregistry.test"},
		Path:    t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	evt := dumps.Event{ID: "z", Kind: "dump", Ctx: dumps.Context{Site: "whitewaters", Type: "fpm"}}
	n := notificationForDump(evt)
	if n.URL != "#sites/theregistry.test/dumps" {
		t.Errorf("URL = %q, want #sites/theregistry.test/dumps", n.URL)
	}
}
