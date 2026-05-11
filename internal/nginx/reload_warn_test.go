package nginx

import (
	"bytes"
	"errors"
	"testing"
)

func TestReloadOrWarn_SilentOnSuccess(t *testing.T) {
	var buf bytes.Buffer
	reloadOrWarn(func() error { return nil }, &buf, "")
	if buf.Len() != 0 {
		t.Fatalf("expected no output on success, got %q", buf.String())
	}
}

func TestReloadOrWarn_PrintsOnFailure(t *testing.T) {
	var buf bytes.Buffer
	reloadOrWarn(func() error { return errors.New("boom") }, &buf, "")
	if got, want := buf.String(), "[WARN] nginx reload: boom\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReloadOrWarn_HonoursIndent(t *testing.T) {
	var buf bytes.Buffer
	reloadOrWarn(func() error { return errors.New("boom") }, &buf, "  ")
	if got, want := buf.String(), "  [WARN] nginx reload: boom\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
