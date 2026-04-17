package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/config"
	phpPkg "github.com/geodro/lerd/internal/php"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
)

// settingsRow describes one focusable line in the settings view.
type settingsRow struct {
	kind       settingsKind
	label      string
	on         bool
	phpVersion string // PHP version, for xdebug rows
}

type settingsKind int

const (
	settingsLANExpose settingsKind = iota
	settingsAutostart
	settingsXdebug
)

func (m *Model) settingsRows() []settingsRow {
	cfg, _ := config.LoadGlobal()
	var rows []settingsRow

	lanExposed := cfg != nil && cfg.LAN.Exposed
	rows = append(rows, settingsRow{
		kind:  settingsLANExpose,
		label: "LAN expose (open every service to the local network)",
		on:    lanExposed,
	})
	rows = append(rows, settingsRow{
		kind:  settingsAutostart,
		label: "Autostart lerd on login",
		on:    lerdSystemd.IsAutostartEnabled(),
	})

	if versions, err := phpPkg.ListInstalled(); err == nil {
		for _, v := range versions {
			rows = append(rows, settingsRow{
				kind:       settingsXdebug,
				label:      "Xdebug · PHP " + v,
				on:         cfg != nil && cfg.IsXdebugEnabled(v),
				phpVersion: v,
			})
		}
	}
	return rows
}

func (m *Model) settingsToggle(rows []settingsRow) tea.Cmd {
	if len(rows) == 0 {
		return nil
	}
	if m.settingsRow >= len(rows) {
		m.settingsRow = len(rows) - 1
	}
	row := rows[m.settingsRow]
	switch row.kind {
	case settingsLANExpose:
		verb := "on"
		if row.on {
			verb = "off"
		}
		m.setStatus("toggling LAN expose "+verb+"…", 5*time.Second)
		return runLerd("", "lan", "expose", verb)
	case settingsAutostart:
		sub := "enable"
		if row.on {
			sub = "disable"
		}
		m.setStatus("autostart "+sub+"…", 5*time.Second)
		return runLerd("", "autostart", sub)
	case settingsXdebug:
		verb := "on"
		if row.on {
			verb = "off"
		}
		m.setStatus("xdebug "+verb+" PHP "+row.phpVersion+"…", 5*time.Second)
		return runLerd("", "xdebug", verb, row.phpVersion)
	}
	return nil
}
