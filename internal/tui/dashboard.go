package tui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/stats"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
)

// statsPollInterval is how often the background poller refreshes resource
// stats. Matches lerd-ui's handleStats cache TTL so the two surfaces show
// the same data at the same cadence; a faster TUI poll would just churn
// `podman stats` without users noticing.
const statsPollInterval = 3 * time.Second

// statsMsg delivers a fresh stats snapshot from the background goroutine to
// the bubbletea program. The poller is started by Run alongside the dumps
// listener so the dashboard pane always has data to render.
type statsMsg struct{ snap stats.Snapshot }

// runStatsPoller fetches a snapshot via stats.Cached on every tick and
// forwards it to the program. Going through Cached (rather than Read
// directly) means the TUI shares lerd-ui's cached snapshot when both are
// running, halving the `podman stats` invocations for the typical
// "dashboard open + TUI open" workflow. Cancelled by ctx so the loop
// exits cleanly on quit.
func runStatsPoller(ctx context.Context, p *tea.Program) {
	ticker := time.NewTicker(statsPollInterval)
	defer ticker.Stop()
	// First read happens immediately so the user doesn't see "no stats"
	// for a full tick after entering the dashboard.
	p.Send(statsMsg{snap: stats.Cached(statsPollInterval)})
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.Send(statsMsg{snap: stats.Cached(statsPollInterval)})
		}
	}
}

// dashboardContentLinesWithCursor renders the Dashboard detail pane. It is
// purely informational — no toggles live here because the System pane (Y)
// already owns every reversible action. Reports a cursor line of 0 so the
// scrollbar treats the view as a plain scroll surface.
func dashboardContentLinesWithCursor(m *Model, _ bool, innerW int) ([]string, int) {
	out := make([]string, 0, 64)
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	add(sectionStyle.Render("Dashboard"))
	add(dimStyle.Render("  press F or esc to return to site detail"))
	add("")

	// Hero: failing workers + update banner.
	hero := []string{}
	if n := countFailingWorkers(m.snap); n > 0 {
		hero = append(hero, failingStyle.Render(fmt.Sprintf("⚠ %d workers failing", n)))
	} else {
		hero = append(hero, runningStyle.Render("all workers healthy"))
	}
	if m.updateAvailable != "" {
		hero = append(hero, accentStyle.Render("update: "+m.updateAvailable))
	} else {
		hero = append(hero, dimStyle.Render("lerd "+m.version))
	}
	add("  " + strings.Join(hero, "   "))
	add("")

	// Counts: sites, services, workers — keep on one line so the user sees
	// the whole instance at a glance without scrolling.
	siteRunning, sitePaused := 0, 0
	for _, s := range m.snap.Sites {
		if s.Paused {
			sitePaused++
		} else if s.FPMRunning {
			siteRunning++
		}
	}
	svcRunning, svcStopped := 0, 0
	workerActive, workerFailing := 0, 0
	for _, s := range m.snap.Services {
		if s.WorkerKind != "" {
			if s.State == stateRunning {
				workerActive++
			}
			continue
		}
		if s.State == stateRunning {
			svcRunning++
		} else {
			svcStopped++
		}
	}
	for _, s := range m.snap.Sites {
		if s.QueueFailing {
			workerFailing++
		}
		if s.ScheduleFailing {
			workerFailing++
		}
		if s.HorizonFailing {
			workerFailing++
		}
		if s.ReverbFailing {
			workerFailing++
		}
		for _, fw := range s.FrameworkWorkers {
			if fw.Failing {
				workerFailing++
			}
		}
		for _, wt := range s.Worktrees {
			for _, fw := range wt.FrameworkWorkers {
				if fw.Failing {
					workerFailing++
				}
			}
		}
	}

	add(sectionStyle.Render("Overview"))
	add(renderSystemInfoRow("Sites",
		fmt.Sprintf("%d total · %s · %s",
			len(m.snap.Sites),
			runningStyle.Render(fmt.Sprintf("%d running", siteRunning)),
			pausedStyle.Render(fmt.Sprintf("%d paused", sitePaused)))))
	add(renderSystemInfoRow("Services",
		fmt.Sprintf("%d total · %s · %s",
			svcRunning+svcStopped,
			runningStyle.Render(fmt.Sprintf("%d running", svcRunning)),
			dimStyle.Render(fmt.Sprintf("%d stopped", svcStopped)))))
	workerLine := runningStyle.Render(fmt.Sprintf("%d active", workerActive))
	if workerFailing > 0 {
		workerLine += " · " + failingStyle.Render(fmt.Sprintf("%d failing (press H)", workerFailing))
	}
	add(renderSystemInfoRow("Workers", workerLine))

	// System health row: DNS / Nginx / Watcher / PHP — mirrors the header
	// but in the dashboard layout so the user has one consolidated view.
	add("")
	add(sectionStyle.Render("System health"))
	add(renderSystemInfoRow("DNS", dnsHealthText(m.snap.Status)))
	add(renderSystemInfoRow("Nginx", runningOrStoppedColoured(m.snap.Status.NginxRunning)))
	add(renderSystemInfoRow("Watcher", runningOrStoppedColoured(m.snap.Status.WatcherRunning)))
	if len(m.snap.Status.PHPRunning) > 0 {
		add(renderSystemInfoRow("PHP FPM", runningStyle.Render(strings.Join(m.snap.Status.PHPRunning, ", "))))
	} else {
		add(renderSystemInfoRow("PHP FPM", dimStyle.Render("none running")))
	}

	// Resources: total CPU + memory + top 5 by memory.
	add("")
	add(sectionStyle.Render("Resources"))
	if !m.stats.Available {
		add(renderSystemInfoRow("Status", dimStyle.Render("collecting…")))
	} else {
		add(renderSystemInfoRow("Total CPU", fmt.Sprintf("%.1f%%", m.stats.TotalCPUPercent)))
		memText := stats.FormatBytes(m.stats.TotalMemBytes)
		if m.stats.HostMemBytes > 0 {
			memText += " / " + stats.FormatBytes(m.stats.HostMemBytes)
		}
		add(renderSystemInfoRow("Total memory", memText))
		add(renderSystemInfoRow("Processes", fmt.Sprintf("%d running (top 5 by load)", len(m.stats.Containers))))
		max := 5
		if len(m.stats.Containers) < max {
			max = len(m.stats.Containers)
		}
		for i := 0; i < max; i++ {
			c := m.stats.Containers[i]
			add("    " + dimStyle.Render(padRight(truncatePlain(c.Name, 24), 24)) +
				" " + fmt.Sprintf("%5.1f%%  %s", c.CPUPercent, stats.FormatBytes(c.MemBytes)))
		}
	}

	// Lerd settings summary so the dashboard mirrors lerd-ui's Lerd Info
	// widget without re-implementing the toggles (those live in System).
	add("")
	add(sectionStyle.Render("Lerd"))
	add(renderSystemInfoRow("Version", m.version))
	add(renderSystemInfoRow("Autostart", onOffWord(lerdSystemd.IsAutostartEnabled())))
	cfg, _ := config.LoadGlobal()
	add(renderSystemInfoRow("LAN expose", onOffWord(cfg != nil && cfg.LAN.Exposed)))
	add(renderSystemInfoRow("Platform", runtime.GOOS+"/"+runtime.GOARCH))

	return out, 0
}

// dnsHealthText renders the DNS pill the same way the header does so the
// dashboard mirrors what users already learnt at-a-glance from the top bar.
func dnsHealthText(s StatusRow) string {
	switch {
	case s.DNSDisabled:
		return dimStyle.Render("disabled (system resolver only)")
	case s.DNSOk:
		return runningStyle.Render("ok")
	case s.DNSDegraded:
		return accentStyle.Render("degraded (system resolver bypassed)")
	default:
		return failingStyle.Render("down")
	}
}

func runningOrStoppedColoured(running bool) string {
	if running {
		return runningStyle.Render("running")
	}
	return dimStyle.Render("stopped")
}

func onOffWord(on bool) string {
	if on {
		return runningStyle.Render("on")
	}
	return dimStyle.Render("off")
}
