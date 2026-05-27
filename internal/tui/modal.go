package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/geodro/lerd/internal/eventbus"
)

// Modal overlays: centered floating boxes that replace the rendered view
// while open. We use full-screen modal mode (Place fills the surrounding
// area with whitespace) rather than per-cell compositing because the
// existing detail-pane swaps (S / Y / F / D / ?) already feel like
// full-screen swaps to the user; modals just give us a more focused
// chrome for high-attention surfaces (palette, picker, help, confirm).

var (
	modalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).
			Padding(0, 2)
	modalTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colTitle)
	modalFooterStyle = lipgloss.NewStyle().Foreground(colDim)
)

// renderModal centers a titled bordered box inside a w x h area. body may
// be multi-line; footer is a single hint line (commit / cancel keys).
// Returns the full w x h frame so callers can hand it back from View().
func renderModal(w, h int, title, body, footer string) string {
	parts := []string{modalTitleStyle.Render(title), "", body}
	if footer != "" {
		parts = append(parts, "", modalFooterStyle.Render(footer))
	}
	box := modalBoxStyle.Render(strings.Join(parts, "\n"))
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}

// modalActive reports whether any modal is currently open. View() consults
// this to decide whether to render the base layout or the active modal.
// Order matters: confirm sits above everything (it's a guard rail), then
// palette, picker, help.
func (m *Model) modalActive() bool {
	return m.confirmActive || m.paletteActive || m.pickerModalActive() || m.helpModalActive
}

// pickerModalActive reports whether the inline PHP / Node picker is open
// in modal-overlay mode. The picker state lives in the existing pickerKind
// field; "modal" mode is the new presentation — we always overlay rather
// than inline-expand below the focused row.
func (m *Model) pickerModalActive() bool {
	return m.pickerKind != kindInfo
}

// renderActiveModal returns the modal frame for the topmost open modal.
// Caller must have checked modalActive() first; this panics on no active
// modal because that would be a programming bug, not user input.
func (m *Model) renderActiveModal(w, h int) string {
	switch {
	case m.confirmActive:
		return m.renderConfirmModal(w, h)
	case m.paletteActive:
		return m.renderPaletteModal(w, h)
	case m.helpModalActive:
		return m.renderHelpModal(w, h)
	case m.pickerModalActive():
		return m.renderPickerModal(w, h)
	}
	return ""
}

// renderConfirmModal draws the y / n confirmation prompt. confirmTitle is
// the heading (e.g. "Remove domain") and confirmBody describes the
// specific subject (e.g. "Remove foo.test? This unlinks the alias from
// nginx and dnsmasq.").
func (m *Model) renderConfirmModal(w, h int) string {
	footer := renderKeyChips("y", "confirm", "n", "cancel", "esc", "cancel")
	return renderModal(w, h, m.confirmTitle, m.confirmBody, footer)
}

// openConfirm stages a confirmation modal. The caller passes the title
// (heading), body (one or two sentences describing what will happen), and
// the tea.Cmd to fire on `y`. Used wherever the TUI offers a single-key
// destructive action — see removeFocusedDomain for the canonical caller.
func (m *Model) openConfirm(title, body string, action tea.Cmd) {
	m.confirmActive = true
	m.confirmTitle = title
	m.confirmBody = body
	m.confirmAction = action
}

// closeConfirm dismisses the prompt without running the action. Always
// safe; no-op when no confirm is open.
func (m *Model) closeConfirm() {
	m.confirmActive = false
	m.confirmTitle = ""
	m.confirmBody = ""
	m.confirmAction = nil
}

// handleConfirmKey resolves the confirmation prompt. y runs the staged
// action; n or esc dismisses without running. We intentionally only honour
// these three keys — anything else is a no-op so typos don't accidentally
// confirm. ctrl+c still quits cleanly even with a confirm open.
func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		action := m.confirmAction
		m.closeConfirm()
		return m, action
	case "n", "N", "esc":
		m.closeConfirm()
		return m, nil
	case "ctrl+c":
		m.logTail.Stop()
		return m, tea.Quit
	}
	return m, nil
}

// handlePickerKey owns every keystroke while the PHP / Node picker is
// open. Before this handler existed the picker only intercepted enter and
// esc, so single-letter shortcuts (O, F, S, Y, D, ?, :, 1-4, c, T, …)
// leaked through to the global handler and silently mutated state behind
// the modal overlay. Tab/shift+tab used to move focus off paneDetail
// while the picker stayed drawn, orphaning it. Now every key the picker
// doesn't understand is a no-op so the user must explicitly cancel or
// apply before any other action fires.
func (m *Model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closePicker()
		return m, nil
	case "enter", " ":
		return m, m.applyPicker()
	case "up", "k":
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}
		return m, nil
	case "down", "j":
		if m.pickerCursor < len(m.pickerOptions)-1 {
			m.pickerCursor++
		}
		return m, nil
	case "home", "g":
		m.pickerCursor = 0
		return m, nil
	case "end", "G":
		if n := len(m.pickerOptions); n > 0 {
			m.pickerCursor = n - 1
		}
		return m, nil
	case "ctrl+c":
		m.logTail.Stop()
		return m, tea.Quit
	case "q":
		// Quit cleanly even from inside the picker, matching the help
		// modal's contract.
		m.logTail.Stop()
		eventbus.Default.Unsubscribe(m.sub)
		return m, tea.Quit
	}
	return m, nil
}

// handleHelpModalKey scrolls the keybinding reference and dismisses on
// `?` (toggle off) or esc. Mirrors the previous detailHelp pane-swap
// behaviour but routed at the top level since the help is now an overlay.
func (m *Model) handleHelpModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "?":
		m.helpModalActive = false
		return m, nil
	case "q":
		// q still quits while a help modal is open; mirrors the global
		// quit binding so the user isn't trapped by the overlay. Match
		// the canonical quit path's unsubscribe + log-tail stop so the
		// eventbus subscriber doesn't leak.
		m.helpModalActive = false
		m.logTail.Stop()
		eventbus.Default.Unsubscribe(m.sub)
		return m, tea.Quit
	case "up", "k":
		if m.helpScroll > 0 {
			m.helpScroll--
		}
		return m, nil
	case "down", "j":
		m.helpScroll++
		return m, nil
	case "pgup":
		m.helpScroll -= 10
		if m.helpScroll < 0 {
			m.helpScroll = 0
		}
		return m, nil
	case "pgdown":
		m.helpScroll += 10
		return m, nil
	case "home", "g":
		m.helpScroll = 0
		return m, nil
	case "ctrl+c":
		m.logTail.Stop()
		return m, tea.Quit
	}
	return m, nil
}

// renderPaletteModal draws the command palette as a centered overlay: the
// `$ lerd <input>▌` prompt on top, suggestions listed below, footer with
// key hints. Replaces the prior inline status-bar prompt so the user can
// scan up to ~12 suggestions at once instead of squinting at the single
// row that fit at the bottom of the screen.
func (m *Model) renderPaletteModal(w, h int) string {
	prompt := accentStyle.Render("$ lerd ") + m.paletteInput + "▌"
	suggestions := paletteSuggestions(m.paletteInput, paletteCommands, 12)

	bodyLines := []string{prompt, ""}
	if len(suggestions) == 0 {
		if strings.TrimSpace(m.paletteInput) == "" {
			bodyLines = append(bodyLines, dimStyle.Render("start typing — tab completes the matching command"))
		} else {
			bodyLines = append(bodyLines, dimStyle.Render("no matching command"))
		}
	} else {
		for i, s := range suggestions {
			marker := "  "
			styled := s
			if i == 0 {
				marker = accentStyle.Render("→ ")
				styled = selectedStyle.Render(s)
			}
			bodyLines = append(bodyLines, marker+styled)
		}
	}

	return renderModal(w, h,
		"Run lerd command",
		strings.Join(bodyLines, "\n"),
		renderKeyChips("tab", "complete", "enter", "run", "esc", "cancel"))
}

// renderHelpModal draws the help reference centered. Reuses the existing
// helpContentLines builder so the modal stays in sync with whatever the
// help reference already lists; we just slice off the leading hint and
// add a footer-driven dismiss prompt.
func (m *Model) renderHelpModal(w, h int) string {
	// Reserve modal padding (~10 cells horizontal) so long lines wrap
	// cleanly instead of being clipped by the box.
	innerW := w - 10
	if innerW < 40 {
		innerW = 40
	}
	all := helpContentLines(m, innerW)
	if m.helpScroll > len(all)-1 {
		m.helpScroll = max(0, len(all)-1)
	}
	visible := all[m.helpScroll:]
	// Cap the visible window so the modal fits vertically.
	maxRows := h - 8
	if maxRows < 5 {
		maxRows = 5
	}
	if len(visible) > maxRows {
		visible = visible[:maxRows]
	}
	return renderModal(w, h,
		"Keybindings",
		strings.Join(visible, "\n"),
		renderKeyChips("↑↓", "scroll", "pgup/pgdn", "page", "?", "close", "esc", "close"))
}

// renderPickerModal draws the PHP / Node version picker as a centered
// overlay. Replaces the prior inline expand-below-the-row presentation;
// nothing changes about how the picker is opened or applied — only the
// chrome is more focused.
func (m *Model) renderPickerModal(w, h int) string {
	var title string
	switch m.pickerKind {
	case kindPHP, kindWorktreePHP:
		title = "Select PHP version"
	case kindNode, kindWorktreeNode:
		title = "Select Node version"
	default:
		title = "Select"
	}
	if m.pickerWorktreeName != "" {
		title += " · worktree " + m.pickerWorktreeName
	}

	lines := make([]string, 0, len(m.pickerOptions)+1)
	if len(m.pickerOptions) == 0 {
		lines = append(lines, dimStyle.Render("no versions installed"))
	}
	for i, opt := range m.pickerOptions {
		marker := "  "
		styled := opt
		if i == m.pickerCursor {
			marker = accentStyle.Render("▸ ")
			styled = selectedStyle.Render(opt)
		}
		lines = append(lines, marker+styled)
	}
	return renderModal(w, h,
		title,
		strings.Join(lines, "\n"),
		renderKeyChips("↑↓", "select", "enter", "apply", "esc", "cancel"))
}
