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
	addStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	delStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	metaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("4")) // blue
)

// replaceAction represents the user's choice when a definition conflict is found.
type replaceAction int

const (
	replaceSkip        replaceAction = iota // keep both as-is
	replaceFromProject                      // apply .lerd.yaml → disk
	replaceFromDisk                         // apply disk → .lerd.yaml
)

// confirmReplace compares existing (on disk) and replacement (from .lerd.yaml)
// by marshalling both to YAML. If they are identical it returns replaceSkip
// immediately. Otherwise it prints a unified diff and asks the user which
// direction to sync, or to skip.
func confirmReplace(kind, name string, existing, replacement interface{}) (replaceAction, error) {
	oldYAML, err := yaml.Marshal(existing)
	if err != nil {
		return replaceSkip, err
	}
	newYAML, err := yaml.Marshal(replacement)
	if err != nil {
		return replaceSkip, err
	}

	if string(oldYAML) == string(newYAML) {
		return replaceSkip, nil // identical — nothing to do
	}

	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(oldYAML)),
		B:        difflib.SplitLines(string(newYAML)),
		FromFile: fmt.Sprintf("%s/%s (current)", kind, name),
		ToFile:   fmt.Sprintf("%s/%s (.lerd.yaml)", kind, name),
		Context:  3,
	})
	if err != nil {
		return replaceSkip, err
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

	choice := 0
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[int]().
				Title(fmt.Sprintf("How to resolve %s/%s?", kind, name)).
				Options(
					huh.NewOption("Use version from .lerd.yaml (update local definition)", int(replaceFromProject)),
					huh.NewOption("Use local definition (update .lerd.yaml)", int(replaceFromDisk)),
					huh.NewOption("Skip (keep both as-is)", int(replaceSkip)),
				).
				Value(&choice),
		),
	).WithTheme(huh.ThemeCatppuccin()).Run(); err != nil {
		return replaceSkip, err
	}

	return replaceAction(choice), nil
}
