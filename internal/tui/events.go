package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/eventbus"
)

// refreshMsg arrives on every tick and on every eventbus publish. Update's
// handler reloads the snapshot off the main loop.
type refreshMsg struct{}

// snapshotMsg carries a freshly-loaded Snapshot from a background goroutine
// back into the tea program.
type snapshotMsg struct{ snap Snapshot }

// tickCmd schedules the next refreshMsg. Called from Update after each
// snapshot lands so the model polls at a steady cadence even when no
// eventbus traffic arrives (cross-process changes show up within the
// interval).
func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return refreshMsg{} })
}

// loadCmd runs loadSnapshot off the main loop. siteinfo.LoadAll and podman
// calls can block for 100s of ms on slow systems; running them in the Update
// handler would freeze input.
func loadCmd() tea.Cmd {
	return func() tea.Msg { return snapshotMsg{snap: loadSnapshot()} }
}

// busCmd subscribes to the in-process eventbus and emits a refreshMsg the
// first time a publish lands. The caller chains busCmd to itself from Update
// so the subscription is long-lived.
func busCmd(sub *eventbus.Subscriber) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-sub.C
		if !ok {
			return nil
		}
		return refreshMsg{}
	}
}
