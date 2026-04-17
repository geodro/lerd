package tui

import (
	"runtime"
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
	settingsWorkerMode
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

	// Worker runtime mode: macOS only. On Linux workers always run via
	// podman exec under systemd so the setting is meaningless there and
	// is hidden from the UI.
	if runtime.GOOS == "darwin" {
		containerMode := cfg != nil && cfg.WorkerExecMode() == config.WorkerExecModeContainer
		label := "Workers in container mode (one container per worker)"
		if !containerMode {
			label = "Workers in exec mode (lower memory, shared FPM container)"
		}
		rows = append(rows, settingsRow{
			kind:  settingsWorkerMode,
			label: label,
			on:    containerMode,
		})
	}

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
	case settingsWorkerMode:
		// Toggle between exec (off) and container (on). Mirrors
		// `lerd workers mode <value>`. Does not stop running workers —
		// caller should restart them for the change to take effect.
		target := config.WorkerExecModeContainer
		if row.on {
			target = config.WorkerExecModeExec
		}
		m.setStatus("switching worker mode to "+target+"…", 5*time.Second)
		return runLerd("", "workers", "mode", target)
	}
	return nil
}
