package tui

// helpSection groups related keybindings under a heading.
type helpSection struct {
	title string
	rows  [][2]string // {key, description}
}

// helpReference is the canonical list of every keybinding in the TUI. It
// lives here rather than inline in panes.go so a future keymap overhaul
// (remappable keys, user-defined shortcuts) has one place to edit.
var helpReference = []helpSection{
	{
		title: "Navigation",
		rows: [][2]string{
			{"tab / shift+tab", "cycle focus through Sites · Services · Detail"},
			{"↑ ↓  j k", "move selection within the focused pane"},
			{"pgup / pgdn", "jump by 10 rows"},
			{"home / end · g G", "jump to first / last row"},
		},
	},
	{
		title: "Filter & sort",
		rows: [][2]string{
			{"/", "type to filter the focused list by name"},
			{"  enter", "apply filter and leave input mode"},
			{"  esc", "clear filter and leave input mode"},
			{"o", "cycle sort order (name · status · …)"},
		},
	},
	{
		title: "Actions",
		rows: [][2]string{
			{"space / enter", "toggle the focused detail row (worker, HTTPS, LAN share, PHP, Node)"},
			{"s", "start / unpause the focused site or start the focused service"},
			{"x", "stop / pause the focused site or stop / remove the focused domain"},
			{"r", "restart the focused site or service"},
			{"p", "pause / unpause toggle for a site"},
			{"t", "open an interactive shell inside the focused container"},
		},
	},
	{
		title: "Domains",
		rows: [][2]string{
			{"a", "add a new domain to the focused site (inline input)"},
			{"e", "edit / rename the focused domain row (add new + remove old)"},
			{"x", "remove the focused domain"},
		},
	},
	{
		title: "Logs",
		rows: [][2]string{
			{"l", "toggle the logs pane for the focused item"},
			{"[ / ]", "cycle through the site's log sources (FPM, workers, app logs)"},
		},
	},
	{
		title: "Panes & overlays",
		rows: [][2]string{
			{"v", "show / hide the services pane"},
			{"S", "swap the detail pane for global Settings (LAN expose, autostart, Xdebug)"},
			{"?", "swap the detail pane for this help reference"},
			{"esc", "close picker or return to site detail"},
		},
	},
	{
		title: "General",
		rows: [][2]string{
			{"R", "force a manual refresh"},
			{"q / ctrl+c", "quit"},
		},
	},
}

// helpContentLines builds the lines drawn inside the detail pane when
// detailMode == detailHelp. Same padding / clipping convention as the other
// detail renderers so the border wraps cleanly.
func helpContentLines(m *Model, innerW int) []string {
	out := make([]string, 0, 64)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	add(sectionStyle.Render("Keybindings"))
	add(dimStyle.Render("  press ? or esc to return to site detail"))
	add("")

	for i, sec := range helpReference {
		if i > 0 {
			add("")
		}
		add(sectionStyle.Render(sec.title))
		for _, row := range sec.rows {
			key := padRight(truncatePlain(row[0], 18), 18)
			add("  " + accentStyle.Render(key) + "  " + row[1])
		}
	}
	return out
}
