package tui

import "github.com/charmbracelet/lipgloss"

var (
	colTitle    = lipgloss.Color("205")
	colDim      = lipgloss.Color("243")
	colDivider  = lipgloss.Color("237")
	colRunning  = lipgloss.Color("42")
	colStopped  = lipgloss.Color("243")
	colFailing  = lipgloss.Color("203")
	colPaused   = lipgloss.Color("214")
	colAccent   = lipgloss.Color("147")
	colSelected = lipgloss.Color("205")
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(colTitle)
	sectionStyle  = lipgloss.NewStyle().Bold(true).Foreground(colDim)
	dimStyle      = lipgloss.NewStyle().Foreground(colDim)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(colSelected)
	focusedPane   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).Padding(0, 1)
	unfocusedPane = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colDivider).Padding(0, 1)
	runningStyle  = lipgloss.NewStyle().Foreground(colRunning)
	stoppedStyle  = lipgloss.NewStyle().Foreground(colStopped)
	failingStyle  = lipgloss.NewStyle().Foreground(colFailing).Bold(true)
	pausedStyle   = lipgloss.NewStyle().Foreground(colPaused)
	accentStyle   = lipgloss.NewStyle().Foreground(colAccent)
	helpStyle     = lipgloss.NewStyle().Foreground(colDim)
)

const (
	glyphRunning = "●"
	glyphStopped = "○"
	glyphFailing = "✖"
	glyphPaused  = "◐"
)
