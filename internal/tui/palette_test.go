package tui

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePaletteArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"status", []string{"status"}},
		{"service restart redis", []string{"service", "restart", "redis"}},
		{"  spaced   out  ", []string{"spaced", "out"}},
		{"", []string{}},
	}
	for _, c := range cases {
		got := parsePaletteArgs(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parsePaletteArgs(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestOpenPalette_SetsActive(t *testing.T) {
	m := NewModel("test")
	m.openPalette()
	if !m.paletteActive {
		t.Error("expected paletteActive=true after openPalette")
	}
	if m.paletteInput != "" {
		t.Errorf("expected blank input on open, got %q", m.paletteInput)
	}
}

func TestModalActive_FalseWhenAllClosed(t *testing.T) {
	m := NewModel("test")
	if m.modalActive() {
		t.Error("expected modalActive=false on a fresh model")
	}
}

func TestShQuote_HandlesEmbeddedSingleQuotes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"it's tricky", `'it'\''s tricky'`},
		{"", "''"},
	}
	for _, c := range cases {
		if got := shQuote(c.in); got != c.want {
			t.Errorf("shQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShQuoteAll_JoinsWithSpaces(t *testing.T) {
	args := []string{"service", "restart", "redis-7"}
	want := "'service' 'restart' 'redis-7'"
	if got := shQuoteAll(args); got != want {
		t.Errorf("shQuoteAll(%v) = %q, want %q", args, got, want)
	}
}

func TestPaletteSuggestions_PrefixMatch(t *testing.T) {
	all := []string{"status", "stop", "start", "service start", "service stop"}
	got := paletteSuggestions("st", all, 10)
	want := []string{"start", "status", "stop"}
	if len(got) != len(want) {
		t.Fatalf("got %d suggestions, want %d (%v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("idx %d: got %q want %q", i, got[i], w)
		}
	}
}

func TestPaletteSuggestions_EmptyInputReturnsNothing(t *testing.T) {
	all := []string{"status", "stop"}
	if got := paletteSuggestions("", all, 10); len(got) != 0 {
		t.Errorf("empty input should not list catalog, got %v", got)
	}
}

func TestPaletteSuggestions_RespectsMax(t *testing.T) {
	all := []string{"a1", "a2", "a3", "a4", "a5"}
	got := paletteSuggestions("a", all, 3)
	if len(got) != 3 {
		t.Errorf("expected max=3 suggestions, got %d", len(got))
	}
}

func TestCompletePaletteInput_SingleMatchCompletesWithSpace(t *testing.T) {
	all := []string{"status", "stop", "doctor"}
	got := completePaletteInput("stat", all)
	if got != "status " {
		t.Errorf("single match should complete to %q, got %q", "status ", got)
	}
}

func TestCompletePaletteInput_MultiMatchAdvancesToCommonPrefix(t *testing.T) {
	all := []string{"service start", "service stop", "service restart"}
	got := completePaletteInput("se", all)
	if got != "service " {
		t.Errorf("multi match should fill common prefix %q, got %q", "service ", got)
	}
}

func TestCompletePaletteInput_NoMatchLeavesInputAlone(t *testing.T) {
	all := []string{"status", "stop"}
	got := completePaletteInput("xyzzy", all)
	if got != "xyzzy" {
		t.Errorf("no match should not mutate input, got %q", got)
	}
}

func TestLongestCommonPrefix(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"service", "service stop", "service start"}, "service"},
		{[]string{"a", "ab", "abc"}, "a"},
		{[]string{"dump on", "dump off"}, "dump o"},
		{[]string{"start", "stop"}, "st"},
		{[]string{"only"}, "only"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := longestCommonPrefix(c.in); got != c.want {
			t.Errorf("longestCommonPrefix(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderPaletteModal_ShowsPromptAndInput(t *testing.T) {
	m := NewModel("test")
	m.openPalette()
	m.paletteInput = "status"
	got := stripANSI(m.renderPaletteModal(120, 30))
	if got == "" {
		t.Fatal("expected non-empty palette modal when active")
	}
	if !strings.Contains(got, "lerd") || !strings.Contains(got, "status") {
		t.Errorf("expected lerd prompt + input, got %q", got)
	}
	if !strings.Contains(got, "Run lerd command") {
		t.Errorf("expected modal title, got %q", got)
	}
}

func TestRenderPaletteModal_ListsSuggestionsForPrefix(t *testing.T) {
	m := NewModel("test")
	m.openPalette()
	m.paletteInput = "se"
	got := stripANSI(m.renderPaletteModal(120, 30))
	if !strings.Contains(got, "service") {
		t.Errorf("expected suggestions to include service*, got %q", got)
	}
}
