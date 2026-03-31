package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"
)

var (
	addStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))  // green
	delStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))  // red
	metaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("4")) // blue
)

// confirmReplace compares existing and replacement by marshalling both to YAML.
// If they are identical it returns true immediately (no prompt needed).
// If they differ it prints a unified diff and asks the user whether to replace.
// Returns true if the replacement should be applied, false to skip.
func confirmReplace(kind, name string, existing, replacement interface{}) (bool, error) {
	oldYAML, err := yaml.Marshal(existing)
	if err != nil {
		return false, err
	}
	newYAML, err := yaml.Marshal(replacement)
	if err != nil {
		return false, err
	}

	if string(oldYAML) == string(newYAML) {
		return true, nil // identical — nothing to do
	}

	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(oldYAML)),
		B:        difflib.SplitLines(string(newYAML)),
		FromFile: fmt.Sprintf("%s/%s (current)", kind, name),
		ToFile:   fmt.Sprintf("%s/%s (.lerd.yaml)", kind, name),
		Context:  3,
	})
	if err != nil {
		return false, err
	}

	fmt.Printf("\n%s %s/%s already exists and differs:\n\n", metaStyle.Render("~"), kind, name)
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			fmt.Println(addStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			fmt.Println(delStyle.Render(line))
		case strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			fmt.Println(metaStyle.Render(line))
		default:
			fmt.Println(line)
		}
	}
	fmt.Println()

	replace := false
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Replace %s/%s with the version from .lerd.yaml?", kind, name)).
				Value(&replace),
		),
	).WithTheme(huh.ThemeCatppuccin()).Run(); err != nil {
		return false, err
	}

	return replace, nil
}
