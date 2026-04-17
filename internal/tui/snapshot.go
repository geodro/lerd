package tui

import (
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteinfo"
)

// Snapshot is the full view-model the TUI renders from. Produced by
// loadSnapshot from the same sources lerd-ui uses, so every pane reflects the
// same point-in-time state.
type Snapshot struct {
	Sites    []siteinfo.EnrichedSite
	Services []ServiceRow
	Status   StatusRow
}

// ServiceRow is a flat row for the services pane. Sourced from podman unit
// status and service config so the TUI doesn't need to reach into ui/server.go.
// For site-owned workers (queue, schedule, horizon, reverb, custom framework
// workers) WorkerSite / WorkerKind are set so actions can run the right
// per-site command instead of `lerd service start/stop` which would fail.
type ServiceRow struct {
	Name      string
	State     ServiceState
	Custom    bool
	Pinned    bool
	SiteCount int
	Dashboard string
	DependsOn []string

	WorkerSite string
	WorkerKind string
	WorkerPath string
}

// ServiceState is a tri-state the TUI uses to pick a glyph and colour.
type ServiceState int

const (
	stateStopped ServiceState = iota
	stateRunning
	statePaused
)

// StatusRow drives the top header bar.
type StatusRow struct {
	DNSOk          bool
	TLD            string
	NginxRunning   bool
	WatcherRunning bool
	PHPRunning     []string
	Version        string
}

// loadSnapshot collects everything the TUI needs in one shot. Called on every
// refresh tick and on every eventbus publish.
func loadSnapshot() Snapshot {
	snap := Snapshot{}

	enriched, err := siteinfo.LoadAll(siteinfo.EnrichUI)
	if err == nil {
		_ = siteinfo.PersistVersionChanges(enriched)
		sort.Slice(enriched, func(i, j int) bool { return enriched[i].Name < enriched[j].Name })
		snap.Sites = enriched
	}

	snap.Services = loadServices()
	// Workers live on sites but belong in the services pane too — same
	// operational surface lerd-ui surfaces as "active" rows for queue,
	// schedule, etc. Append them after the shared services so the user
	// sees the full running-units picture in one list.
	snap.Services = append(snap.Services, workerRows(snap.Sites)...)
	snap.Status = loadStatus()
	return snap
}

// workerRows synthesises one ServiceRow per active / defined worker across
// every enriched site. Naming matches the web UI (`queue-<site>`, etc.) so
// users moving between surfaces see familiar identifiers.
func workerRows(sites []siteinfo.EnrichedSite) []ServiceRow {
	var out []ServiceRow
	add := func(site siteinfo.EnrichedSite, kind string, running, failing bool) {
		state := stateStopped
		if failing {
			state = stateStopped
		} else if running {
			state = stateRunning
		}
		out = append(out, ServiceRow{
			Name:       kind + "-" + site.Name,
			State:      state,
			WorkerSite: site.Name,
			WorkerKind: kind,
			WorkerPath: site.Path,
			SiteCount:  1,
		})
	}
	for _, s := range sites {
		if s.HasQueueWorker {
			add(s, "queue", s.QueueRunning, s.QueueFailing)
		}
		if s.HasScheduleWorker {
			add(s, "schedule", s.ScheduleRunning, s.ScheduleFailing)
		}
		if s.HasHorizon {
			add(s, "horizon", s.HorizonRunning, s.HorizonFailing)
		}
		if s.HasReverb {
			add(s, "reverb", s.ReverbRunning, s.ReverbFailing)
		}
		for _, fw := range s.FrameworkWorkers {
			switch fw.Name {
			case "queue", "schedule", "horizon", "reverb":
				continue
			}
			add(s, fw.Name, fw.Running, fw.Failing)
		}
	}
	return out
}

func loadServices() []ServiceRow {
	rows := make([]ServiceRow, 0, 16)
	for _, name := range siteinfo.KnownServices {
		rows = append(rows, buildServiceRow(name, false))
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		r := buildServiceRow(svc.Name, true)
		r.DependsOn = svc.DependsOn
		r.Dashboard = svc.Dashboard
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

func buildServiceRow(name string, custom bool) ServiceRow {
	unit := "lerd-" + name
	status, _ := podman.UnitStatus(unit)
	state := stateStopped
	switch status {
	case "active", "activating":
		state = stateRunning
	}
	if config.ServiceIsPaused(name) {
		state = statePaused
	}
	return ServiceRow{
		Name:      name,
		State:     state,
		Custom:    custom,
		Pinned:    config.ServiceIsPinned(name),
		SiteCount: config.CountSitesUsingService(name),
	}
}

func loadStatus() StatusRow {
	cfg, _ := config.LoadGlobal()
	tld := "test"
	if cfg != nil {
		tld = cfg.DNS.TLD
	}
	dnsOK, _ := dns.Check(tld)
	row := StatusRow{
		TLD:          tld,
		DNSOk:        dnsOK,
		NginxRunning: podman.Cache.Running("lerd-nginx"),
	}

	if versions, err := phpPkg.ListInstalled(); err == nil {
		for _, v := range versions {
			short := strings.ReplaceAll(v, ".", "")
			if podman.Cache.Running("lerd-php" + short + "-fpm") {
				row.PHPRunning = append(row.PHPRunning, v)
			}
		}
	}

	st, _ := podman.UnitStatus("lerd-watcher")
	row.WatcherRunning = st == "active" || st == "activating"

	return row
}
