package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/siteinfo"
)

func TestNextFocus_SkipsServicesWhenHidden(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap()
	m.hideServices = true
	m.focus = paneSites

	got := m.nextFocus(+1)
	if got != paneDetail {
		t.Fatalf("hideServices + tab should skip to detail, got %d", got)
	}
}

func TestNextFocus_NoSitesSkipsDetail(t *testing.T) {
	m := NewModel("test")
	m.snap = Snapshot{
		Services: []ServiceRow{{Name: "mysql", State: stateRunning}},
	}
	m.focus = paneServices
	got := m.nextFocus(+1)
	if got != paneSites {
		t.Fatalf("without sites, tab from services should wrap to sites (skipping detail), got %d", got)
	}
}

func TestClampCursors_ClampsToVisibleCount(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap()
	m.siteCursor = 5
	m.svcCursor = 9
	m.clampCursors()
	if m.siteCursor >= len(m.visibleSites()) {
		t.Fatalf("siteCursor %d out of bounds (len=%d)", m.siteCursor, len(m.visibleSites()))
	}
	if m.svcCursor >= len(m.visibleServices()) {
		t.Fatalf("svcCursor %d out of bounds (len=%d)", m.svcCursor, len(m.visibleServices()))
	}
}

func TestFilterInput_CollectsRunes(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap()
	m.focus = paneSites
	m.filterActive = true

	for _, r := range "be" {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(*Model)
	}
	if m.siteFilter != "be" {
		t.Fatalf("expected filter 'be', got %q", m.siteFilter)
	}
	// Backspace drops one rune.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(*Model)
	if m.siteFilter != "b" {
		t.Fatalf("expected filter 'b' after backspace, got %q", m.siteFilter)
	}
	// Esc clears and exits input mode.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = next.(*Model)
	if m.siteFilter != "" || m.filterActive {
		t.Fatalf("esc should clear filter and leave input, got filter=%q active=%v", m.siteFilter, m.filterActive)
	}
}

func TestDetailMode_SToggleSetsFocus(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap()
	m.focus = paneSites
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = next.(*Model)
	if m.detailMode != detailSettings {
		t.Fatalf("S should enter settings mode, got %d", m.detailMode)
	}
	if m.focus != paneDetail {
		t.Fatalf("S should move focus to detail, got %d", m.focus)
	}
	// S again → back to site
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = next.(*Model)
	if m.detailMode != detailSite {
		t.Fatalf("second S should return to site detail, got %d", m.detailMode)
	}
}

func TestDetailMode_HelpToggle(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = next.(*Model)
	if m.detailMode != detailHelp {
		t.Fatalf("? should enter help mode, got %d", m.detailMode)
	}
	// Esc returns to site detail.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = next.(*Model)
	if m.detailMode != detailSite {
		t.Fatalf("esc should return to site detail, got %d", m.detailMode)
	}
}

func TestHideServicesToggle(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap()
	m.focus = paneServices
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = next.(*Model)
	if !m.hideServices {
		t.Fatalf("v should hide services")
	}
	if m.focus == paneServices {
		t.Fatalf("focus should move off hidden pane, got %d", m.focus)
	}
}

func TestViewRendersUnderSettingsMode(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap()
	m.width, m.height = 150, 40
	m.detailMode = detailSettings
	out := m.View()
	if !strings.Contains(out, "Settings") {
		t.Fatalf("settings view should include 'Settings' header, got:\n%s", out)
	}
}

func TestViewRendersUnderHelpMode(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap()
	m.width, m.height = 150, 50
	m.detailMode = detailHelp
	out := m.View()
	if !strings.Contains(out, "Keybindings") {
		t.Fatalf("help view should include 'Keybindings' header")
	}
}

func TestDomainInput_CommitRunsLerdAdd(t *testing.T) {
	m := NewModel("test")
	m.snap = Snapshot{
		Sites: []siteinfo.EnrichedSite{{Name: "alpha", Path: "/x", Domains: []string{"alpha.test"}}},
	}
	m.focus = paneDetail
	m.openDomainInput()
	for _, r := range "newdomain" {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(*Model)
	}
	if m.domainInput != "newdomain" {
		t.Fatalf("expected pending domain 'newdomain', got %q", m.domainInput)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*Model)
	if cmd == nil {
		t.Fatalf("enter should return a Cmd")
	}
	if m.domainInputActive {
		t.Fatalf("input should be closed after commit")
	}
}

func TestDomainInput_EscCancels(t *testing.T) {
	m := NewModel("test")
	m.snap = Snapshot{
		Sites: []siteinfo.EnrichedSite{{Name: "alpha", Path: "/x", Domains: []string{"alpha.test"}}},
	}
	m.openDomainInput()
	m.domainInput = "partial"
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = next.(*Model)
	if m.domainInputActive || m.domainInput != "" {
		t.Fatalf("esc should clear input, got active=%v value=%q", m.domainInputActive, m.domainInput)
	}
}

func TestOpenDomainEdit_PrefillsShortName(t *testing.T) {
	m := NewModel("test")
	m.openDomainEdit("staging.alpha.test")
	if !strings.Contains(m.domainInput, "staging.alpha") && m.domainInput != "staging.alpha" {
		t.Fatalf("edit should prefill short form, got %q", m.domainInput)
	}
	if m.domainInputEditing != "staging.alpha.test" {
		t.Fatalf("editing target not stored, got %q", m.domainInputEditing)
	}
}

func TestClosePicker(t *testing.T) {
	m := NewModel("test")
	m.pickerKind = kindPHP
	m.pickerOptions = []string{"8.3", "8.4"}
	m.pickerCursor = 1
	m.closePicker()
	if m.pickerKind != kindInfo || m.pickerOptions != nil || m.pickerCursor != 0 {
		t.Fatalf("closePicker should reset state, got %+v", m)
	}
}

func TestWorkerActionCmd_BuiltinWorker(t *testing.T) {
	m := NewModel("test")
	svc := &ServiceRow{
		Name: "queue-alpha", WorkerKind: "queue", WorkerSite: "alpha", WorkerPath: "/x",
	}
	if cmd := m.workerActionCmd(svc, "start"); cmd == nil {
		t.Fatalf("should return a Cmd for a worker row")
	}
	// Non-worker service rows get nil so callers fall through.
	plain := &ServiceRow{Name: "mysql"}
	if cmd := m.workerActionCmd(plain, "start"); cmd != nil {
		t.Fatalf("plain services should return nil (fallthrough)")
	}
}

func TestSiteByName(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap()
	if s := m.siteByName("alpha"); s == nil || s.Name != "alpha" {
		t.Fatalf("siteByName(alpha) failed: %+v", s)
	}
	if s := m.siteByName("missing"); s != nil {
		t.Fatalf("siteByName(missing) should be nil")
	}
}
