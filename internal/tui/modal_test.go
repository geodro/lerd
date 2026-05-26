package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRenderModal_CentersTitleAndFooter(t *testing.T) {
	out := stripANSI(renderModal(80, 24, "Hello", "world", "esc cancel"))
	if !strings.Contains(out, "Hello") {
		t.Errorf("expected title in output:\n%s", out)
	}
	if !strings.Contains(out, "world") {
		t.Errorf("expected body in output:\n%s", out)
	}
	if !strings.Contains(out, "esc cancel") {
		t.Errorf("expected footer in output:\n%s", out)
	}
}

func TestOpenConfirm_SetsState(t *testing.T) {
	m := NewModel("test")
	m.openConfirm("title", "body", nil)
	if !m.confirmActive {
		t.Error("expected confirmActive=true after openConfirm")
	}
	if m.confirmTitle != "title" || m.confirmBody != "body" {
		t.Errorf("title/body mismatch: %q / %q", m.confirmTitle, m.confirmBody)
	}
}

func TestCloseConfirm_ClearsState(t *testing.T) {
	m := NewModel("test")
	m.openConfirm("title", "body", nil)
	m.closeConfirm()
	if m.confirmActive {
		t.Error("expected confirmActive=false after closeConfirm")
	}
	if m.confirmTitle != "" || m.confirmBody != "" {
		t.Errorf("confirm fields should clear, got %q / %q", m.confirmTitle, m.confirmBody)
	}
}

func TestHandleConfirmKey_YRunsAction(t *testing.T) {
	m := NewModel("test")
	ran := false
	m.openConfirm("ok?", "yes?", func() tea.Msg {
		ran = true
		return nil
	})
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("expected cmd to be the staged action")
	}
	cmd()
	if !ran {
		t.Error("expected staged action to run after y")
	}
	if m.confirmActive {
		t.Error("confirm should close after y")
	}
}

func TestHandleConfirmKey_NDismissesWithoutRunning(t *testing.T) {
	m := NewModel("test")
	ran := false
	m.openConfirm("ok?", "yes?", func() tea.Msg {
		ran = true
		return nil
	})
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd != nil {
		t.Errorf("n should not return a command, got %v", cmd)
	}
	if ran {
		t.Error("staged action must not fire after n")
	}
	if m.confirmActive {
		t.Error("confirm should close after n")
	}
}

func TestModalActive_TrueWhenAnyOpen(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*Model)
	}{
		{"confirm", func(m *Model) { m.openConfirm("a", "b", nil) }},
		{"palette", func(m *Model) { m.openPalette() }},
		{"help", func(m *Model) { m.helpModalActive = true }},
		{"picker", func(m *Model) { m.pickerKind = kindPHP }},
	}
	for _, c := range cases {
		m := NewModel("test")
		c.setup(m)
		if !m.modalActive() {
			t.Errorf("%s: expected modalActive=true", c.name)
		}
	}
}

func TestRenderConfirmModal_IncludesTitleAndBody(t *testing.T) {
	m := NewModel("test")
	m.openConfirm("Remove domain", "Remove foo.test from acme?", nil)
	out := stripANSI(m.renderConfirmModal(120, 30))
	if !strings.Contains(out, "Remove domain") {
		t.Errorf("expected title 'Remove domain':\n%s", out)
	}
	if !strings.Contains(out, "foo.test") {
		t.Errorf("expected body to include the subject:\n%s", out)
	}
	if !strings.Contains(out, "confirm") || !strings.Contains(out, "cancel") {
		t.Errorf("expected confirm / cancel chip labels:\n%s", out)
	}
}

func TestRenderPickerModal_HighlightsCursorOption(t *testing.T) {
	m := NewModel("test")
	m.pickerKind = kindPHP
	m.pickerOptions = []string{"8.1", "8.2", "8.3"}
	m.pickerCursor = 1
	out := stripANSI(m.renderPickerModal(100, 24))
	if !strings.Contains(out, "Select PHP version") {
		t.Errorf("expected PHP title:\n%s", out)
	}
	for _, v := range []string{"8.1", "8.2", "8.3"} {
		if !strings.Contains(out, v) {
			t.Errorf("expected version %q in output:\n%s", v, out)
		}
	}
	if !strings.Contains(out, "▸") {
		t.Errorf("expected cursor marker on the selected row:\n%s", out)
	}
}
