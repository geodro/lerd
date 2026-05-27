package tui

import (
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// presetSuggestions mirrors internal/ui/web/src/stores/presetSuggestions.ts:
// a service of the key gets an "install <value> for an admin dashboard"
// hint. The TUI repeats the same map so users see the same nudge on both
// surfaces; eventually this should move into Go config land but keeping a
// local copy avoids a Phase-4-only refactor of an unrelated module.
var presetSuggestions = map[string]string{
	"mysql":         "phpmyadmin",
	"postgres":      "pgadmin",
	"mongo":         "mongo-express",
	"elasticsearch": "elasticvue",
	"typesense":     "typesense-dashboard",
}

// serviceDetailContentLines renders the right-hand pane when focus is on
// the services list. Mirrors the web UI's ServiceDetail layout: a header,
// the running state, dependencies, a per-service env block, the list of
// sites referencing it, and a preset-suggestion banner where one applies.
// Worker rows (queue-X, schedule-X, …) get their own variant since they
// have no own container or env.
func serviceDetailContentLines(m *Model, svc *ServiceRow, innerW int) []string {
	out := make([]string, 0, 32)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	if svc == nil {
		add(sectionStyle.Render("Service detail"))
		add(dimStyle.Render("  no service selected"))
		return out
	}

	if svc.WorkerKind != "" {
		return workerDetailContentLines(svc, innerW)
	}

	// Header: name, version, state.
	add(sectionStyle.Render(svc.Name))
	stateText := serviceStateText(svc.State)
	if svc.Version != "" {
		add(dimStyle.Render("  version: ") + svc.Version)
	}
	add(dimStyle.Render("  state:   ") + stateText)
	add(dimStyle.Render("  unit:    ") + "lerd-" + svc.Name)
	if svc.Pinned {
		add(dimStyle.Render("  pinned:  ") + accentStyle.Render("yes (preset will not auto-update)"))
	}
	if svc.Dashboard != "" {
		add(dimStyle.Render("  dashbd:  ") + svc.Dashboard)
	}
	add("")

	// Dependencies.
	if len(svc.DependsOn) > 0 {
		add(sectionStyle.Render("Depends on"))
		states := m.serviceStatesByName()
		for _, dep := range svc.DependsOn {
			add(renderSiteServiceRow(dep, states[dep]))
		}
		add("")
	}

	// Sites using this service.
	add(sectionStyle.Render("Sites using"))
	sites := config.SitesUsingService(svc.Name)
	if len(sites) == 0 {
		add(dimStyle.Render("  no sites currently reference " + svc.Name))
	} else {
		for _, s := range sites {
			add("  " + accentStyle.Render("·") + " " + s.Name)
		}
	}
	add("")

	// Env vars (templates from the preset or env from a custom service).
	envLines := serviceEnvLines(svc.Name, svc.Custom)
	if len(envLines) > 0 {
		add(sectionStyle.Render("Env vars"))
		for _, ln := range envLines {
			add("  " + dimStyle.Render(ln))
		}
		add("")
	}

	// Preset suggestion banner: if the focused service has an associated
	// admin dashboard preset that isn't installed yet, hint at it. We don't
	// install from the TUI (Preset install is destructive-ish per the TUI
	// scope rule); the banner just points the user at the CLI verb.
	if hint := presetSuggestionFor(svc); hint != "" {
		add(accentStyle.Render("  💡 ") + hint)
		add("")
	}

	// Quick-action hint so the user discovers what's reversible from the
	// services pane: matches what the help reference says.
	add(sectionStyle.Render("Actions"))
	add(dimStyle.Render("  s start · x stop · r restart · t shell · u update · b rollback · l logs"))
	return out
}

// workerDetailContentLines renders the service-detail pane variant for
// worker rows (queue-X, schedule-X, custom-X). Workers run as systemd user
// units inside the owning site's FPM container, so their detail differs
// from regular services: there's no image, no DependsOn, no preset
// suggestion — just the parent site, kind, and unit name.
func workerDetailContentLines(svc *ServiceRow, innerW int) []string {
	out := make([]string, 0, 16)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	add(sectionStyle.Render(svc.Name))
	add(dimStyle.Render("  kind:    ") + svc.WorkerKind)
	add(dimStyle.Render("  site:    ") + svc.WorkerSite)
	add(dimStyle.Render("  state:   ") + serviceStateText(svc.State))
	add(dimStyle.Render("  unit:    ") + "lerd-" + svc.WorkerKind + "-" + svc.WorkerSite)
	if svc.WorkerPath != "" {
		add(dimStyle.Render("  path:    ") + svc.WorkerPath)
	}
	add("")
	add(sectionStyle.Render("Actions"))
	add(dimStyle.Render("  s start · x stop · r restart · t shell (parent site container) · l logs"))
	return out
}

// serviceEnvLines returns the env-var entries declared by a service's
// preset or its custom-service YAML. Mirrors the union of what the web
// UI's ServiceEnvTab and PHP-side env writer see. Lines come back already
// trimmed to "KEY=value" form, with Environment map keys sorted so the
// render is stable across redraws (Go map iteration is randomised, and
// the service-detail pane re-renders every spinner tick).
func serviceEnvLines(name string, custom bool) []string {
	if custom {
		svc, err := config.LoadCustomService(name)
		if err != nil || svc == nil {
			return nil
		}
		out := make([]string, 0, len(svc.EnvVars)+len(svc.Environment))
		out = append(out, svc.EnvVars...)
		keys := make([]string, 0, len(svc.Environment))
		for k := range svc.Environment {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, k+"="+svc.Environment[k])
		}
		return out
	}
	if config.IsDefaultPreset(name) {
		return config.DefaultPresetEnvVars(name)
	}
	return nil
}

// presetSuggestionFor returns a one-line nudge string when the focused
// service has an associated admin-dashboard preset the user hasn't
// installed yet. Returns "" when there's no suggestion or the admin
// service is already running (we detect via custom service registration).
func presetSuggestionFor(svc *ServiceRow) string {
	if svc == nil {
		return ""
	}
	target, ok := presetSuggestions[svc.Name]
	if !ok {
		return ""
	}
	if _, err := config.LoadCustomService(target); err == nil {
		// Already installed — no banner.
		return ""
	}
	return "install " + target + " for a browser dashboard (run `lerd preset install " + target + "`)"
}

// serviceStateText renders a one-word state with the matching colour. Used
// in the service header and the dependency rows; centralised so a future
// state rename only changes here.
func serviceStateText(state ServiceState) string {
	switch state {
	case stateRunning:
		return runningStyle.Render("running")
	case statePaused:
		return pausedStyle.Render("paused")
	default:
		return strings.TrimSpace(dimStyle.Render("stopped"))
	}
}
