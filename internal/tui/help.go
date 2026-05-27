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
			{"tab / shift+tab", "cycle focus through Sites · Detail · Services (use `v` to hide services from the cycle)"},
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
			{"O", "open the focused site's primary domain in the default browser"},
			{"u", "service update — pull a newer image and restart (services pane)"},
			{"b", "service rollback — revert to the previously-running image (services pane)"},
		},
	},
	{
		title: "Site detail tabs",
		rows: [][2]string{
			{"1", "Overview tab (default — workers, toggles, worktrees)"},
			{"2", "Env tab (read-only .env display)"},
			{"3", "Dumps tab (dump() / dd() events filtered to this site)"},
			{"4", "App logs tab (every framework-declared log file with size and mtime)"},
		},
	},
	{
		title: "Dumps view",
		rows: [][2]string{
			{"/", "search the buffer (matches site, request, label, file, text, type)"},
			{"1", "toggle the `fpm` context-filter chip (mutually exclusive with `cli`)"},
			{"2", "toggle the `cli` context-filter chip"},
			{"enter / space", "toggle full-content expansion of the selected entry"},
			{"c", "clear the in-memory buffer and run `lerd dump clear`"},
			{"T", "toggle the dump bridge globally (lerd dump on/off)"},
		},
	},
	{
		title: "Toasts",
		rows: [][2]string{
			{"d", "dismiss the newest toast in the bottom-right corner"},
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
		title: "Worktrees",
		rows: [][2]string{
			{"space / enter", "toggle the focused per-worktree row (worker, isolated DB)"},
		},
	},
	{
		title: "Logs",
		rows: [][2]string{
			{"l", "toggle the logs pane for the focused item"},
			{"[ / ]", "cycle through the site's log sources (FPM, workers, app logs)"},
			{"{ / }", "scroll back through buffered output / return to tail"},
			{"f", "find within the tailed buffer — matches highlighted, non-matches dimmed"},
		},
	},
	{
		title: "Panes & overlays",
		rows: [][2]string{
			{"v", "show / hide the services pane"},
			{"F", "swap the detail pane for the Dashboard (counts, system health, resources)"},
			{"S", "swap the detail pane for global Settings (LAN expose, autostart, Xdebug)"},
			{"Y", "swap the detail pane for the System overview (DNS, Nginx, Watcher, PHP, Node, Lerd)"},
			{"D", "swap the detail pane for the live dump() / dd() feed"},
			{"?", "swap the detail pane for this help reference"},
			{"esc", "close picker or return to site detail"},
		},
	},
	{
		title: "General",
		rows: [][2]string{
			{":", "open the command palette — type any `lerd <args>` and press enter"},
			{"R", "force a manual refresh"},
			{"H", "heal every failed worker (reset-failed + start)"},
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
