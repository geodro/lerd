package tui

import (
	"strings"
	"testing"
)

func TestClassifyLogLine(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello world", ""},
		{"ERROR: bad request", "error"},
		{"unhandled exception thrown", "error"},
		{"[fatal] db down", "error"},
		{"WARN: slow query", "warn"},
		{"warning: deprecated", "warn"},
		{"PANIC", "error"},
	}
	for _, c := range cases {
		if got := classifyLogLine(c.in); got != c.want {
			t.Errorf("classifyLogLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStyleLogLine_PreservesTextWithoutFilter(t *testing.T) {
	got := stripANSI(styleLogLine("hello world", ""))
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected raw text to survive styling, got %q", got)
	}
}

func TestStyleLogLine_PreservesTextWithFilter(t *testing.T) {
	got := stripANSI(styleLogLine("connecting to redis", "redis"))
	if !strings.Contains(got, "connecting to redis") {
		t.Errorf("filter should not drop characters, got %q", got)
	}
}

func TestStyleLogLine_ErrorPreservesText(t *testing.T) {
	got := stripANSI(styleLogLine("ERROR: kaboom", ""))
	if !strings.Contains(got, "ERROR: kaboom") {
		t.Errorf("severity styling must keep the line content, got %q", got)
	}
}

func TestHighlightMatches_UnicodeCaseFolding(t *testing.T) {
	// İ (U+0130) is the Turkish capital with dot; Go's unicode.ToLower
	// folds it to 'i' (U+0069), which is a different byte length. A
	// byte-offset-based splice would over-read; rune-based should not.
	src := "GET /İstanbul HTTP/1.1"
	got := stripANSI(highlightMatches(src, src, "istanbul"))
	if got != src {
		t.Errorf("rune-aware highlight should preserve the original chars\nwant: %q\n got: %q", src, got)
	}
}

func TestHighlightMatches_MultipleOccurrences(t *testing.T) {
	src := "redis redis foo redis"
	got := stripANSI(highlightMatches(src, src, "redis"))
	if got != src {
		t.Errorf("text content must survive multi-match highlight\nwant: %q\n got: %q", src, got)
	}
}
