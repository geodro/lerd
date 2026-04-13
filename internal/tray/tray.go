//go:build !nogui

package tray

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/geodro/lerd/internal/config"
	lerdSystemd "github.com/geodro/lerd/internal/systemd"
	lerdUpdate "github.com/geodro/lerd/internal/update"
	"github.com/geodro/lerd/internal/version"
	"github.com/getlantern/systray"
)

// apiBase is the loopback URL used for polling the lerd HTTP API. It
// deliberately stays on 127.0.0.1 because the tray should work even
// before nginx / DNS are up.
const apiBase = "http://127.0.0.1:7073"

// dashboardURL is the user-facing URL opened by "Open Dashboard" — the
// nginx-proxied vhost so the dashboard shows up under lerd.localhost
// rather than a bare IP:port.
const dashboardURL = "http://lerd.localhost"

// Snapshot holds the state polled from the Lerd API.
type Snapshot struct {
	Running          bool
	NginxRunning     bool
	DNSOK            bool
	PHPVersions      []phpInfo
	PHPDefault       string
	Services         []serviceInfo
	AutostartEnabled bool
	LANExposed       bool   // lerd lan expose state — drives the LAN toggle item
	LatestVersion    string // non-empty (e.g. "v0.8.5") when a newer version is available
}

type phpInfo struct {
	Version string
}

type serviceInfo struct {
	Name               string `json:"name"`
	Status             string `json:"status"`
	Paused             bool   `json:"paused,omitempty"`
	QueueSite          string `json:"queue_site,omitempty"`
	ScheduleWorkerSite string `json:"schedule_worker_site,omitempty"`
	StripeListenerSite string `json:"stripe_listener_site,omitempty"`
	ReverbSite         string `json:"reverb_site,omitempty"`
	HorizonSite        string `json:"horizon_site,omitempty"`
	WorkerSite         string `json:"worker_site,omitempty"`
}

const daemonEnv = "LERD_TRAY_DAEMON"

// Run starts the system tray applet.
// Unless already running as a daemon (or under launchd), it re-execs itself
// detached from the terminal and returns immediately so the shell prompt is
// restored. When launched by launchd, XPC_SERVICE_NAME is set and we run
// foreground directly — launchd owns the process lifecycle.
func Run(mono bool) error {
	if os.Getenv(daemonEnv) == "" && os.Getenv("XPC_SERVICE_NAME") == "" {
		return detach(mono)
	}
	if !acquireLock() {
		// Another instance is already running — exit silently.
		return nil
	}
	systray.Run(func() { onReady(mono) }, nil)
	return nil
}

// acquireLock tries to acquire an exclusive flock on a per-user lock file.
// It returns true if the lock was acquired (safe to start), false if another
// instance already holds it. When true is returned the lock is held for the
// lifetime of the process (the file is intentionally never closed).
func acquireLock() bool {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "lerd-tray.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		// Can't create the lock file — allow startup rather than blocking.
		return true
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return false
	}
	// Write PID for debugging; keep f open to hold the lock.
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	return true
}

// detach re-execs the current binary with the same tray arguments in a new
// session, detached from the controlling terminal.
func detach(mono bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// lerd-tray is a standalone binary — no "tray" subcommand needed.
	var args []string
	if filepath.Base(exe) != "lerd-tray" {
		args = append(args, "tray")
	}
	if mono {
		args = append(args, "--mono")
	} else {
		args = append(args, "--mono=false")
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), daemonEnv+"=1")
	cmd.Stdin = null
	cmd.Stdout = null
	cmd.Stderr = null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func onReady(mono bool) {
	// In mono mode we register a template icon so the OS panel recolors
	// it to match the theme. In colour mode we set the icon directly —
	// the applyLoop will then swap between iconPNG (red, stopped) and
	// iconWhitePNG (white, running) on every state change.
	if mono {
		systray.SetTemplateIcon(iconMonoPNG, iconMonoPNG)
	} else {
		systray.SetIcon(iconPNG)
	}
	// SetTitle would show text next to the icon in the macOS menu bar — skip it.
	systray.SetTooltip("Lerd — local dev environment")

	menu := buildMenu()

	ctx, cancel := context.WithCancel(context.Background())

	updateCh := make(chan *Snapshot, 1)

	go runPoller(ctx, updateCh)
	go applyLoop(menu, updateCh, mono)
	go handleDash(menu.mDash)
	go handleToggle(menu.mToggle)
	go handleServices(menu)
	go handlePHP(menu)
	go handleAutostart(menu.mAutostart)
	if menu.mLAN != nil {
		go handleLAN(menu.mLAN)
	}
	go handleUpdate(menu.mUpdate)
	go handleQuit(menu.mQuit, cancel)
}

func runPoller(ctx context.Context, updateCh chan<- *Snapshot) {
	// Poll immediately, then every 5 s.
	send := func() {
		snap := fetchSnapshot()
		select {
		case updateCh <- snap:
		default:
			// drop if channel full (previous update not consumed yet)
		}
	}
	send()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

func fetchSnapshot() *Snapshot {
	client := &http.Client{Timeout: 4 * time.Second}

	snap := &Snapshot{}

	// /api/status
	type statusResp struct {
		Nginx struct {
			Running bool `json:"running"`
		} `json:"nginx"`
		DNS struct {
			OK bool `json:"ok"`
		} `json:"dns"`
		PHPFPMs []struct {
			Version string `json:"version"`
		} `json:"php_fpms"`
		PHPDefault string `json:"php_default"`
	}
	if r, err := client.Get(apiBase + "/api/status"); err == nil {
		var sr statusResp
		if json.NewDecoder(r.Body).Decode(&sr) == nil {
			snap.Running = sr.Nginx.Running
			snap.NginxRunning = sr.Nginx.Running
			snap.DNSOK = sr.DNS.OK
			snap.PHPDefault = sr.PHPDefault
			for _, p := range sr.PHPFPMs {
				snap.PHPVersions = append(snap.PHPVersions, phpInfo{Version: p.Version})
			}
		}
		r.Body.Close()
	} else {
		// API unreachable — return empty (stopped) snapshot
		return snap
	}

	snap.AutostartEnabled = lerdSystemd.IsAutostartEnabled()

	// LAN exposure state — read directly from config rather than the API
	// because the tray cares about the persisted intent, not the live bind
	// state (which we'd need a new API for).
	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil {
		snap.LANExposed = cfg.LAN.Exposed
	}

	// /api/services — only real services (exclude queue/schedule/stripe per-site workers)
	if r, err := client.Get(apiBase + "/api/services"); err == nil {
		var all []serviceInfo
		if json.NewDecoder(r.Body).Decode(&all) == nil {
			for _, svc := range all {
				if svc.QueueSite != "" || svc.ScheduleWorkerSite != "" ||
					svc.StripeListenerSite != "" || svc.ReverbSite != "" ||
					svc.HorizonSite != "" || svc.WorkerSite != "" {
					continue
				}
				snap.Services = append(snap.Services, svc)
			}
		}
		r.Body.Close()
	}

	// Update check (uses shared disk cache — fast, no extra network call when cache is fresh)
	if info, _ := lerdUpdate.CachedUpdateCheck(version.Version); info != nil {
		snap.LatestVersion = info.LatestVersion
	}

	return snap
}

func applyLoop(menu *menuState, updateCh <-chan *Snapshot, mono bool) {
	// Track the last icon we set so we don't thrash the systray with
	// identical SetIcon calls every 5s poll.
	var lastRunning = -1
	for snap := range updateCh {
		menu.apply(snap)
		// In color mode the icon doubles as a status indicator: white L
		// when lerd is running, red L when stopped. In mono mode the OS
		// recolors the template icon, so we leave it alone.
		if !mono {
			running := 0
			if snap != nil && snap.Running {
				running = 1
			}
			if running != lastRunning {
				if running == 1 {
					systray.SetIcon(iconWhitePNG)
				} else {
					systray.SetIcon(iconPNG)
				}
				lastRunning = running
			}
		}
	}
}

func handleDash(item *systray.MenuItem) {
	for range item.ClickedCh {
		openURL(dashboardURL)
	}
}

func handleToggle(item *systray.MenuItem) {
	for range item.ClickedCh {
		go func() {
			snap := fetchSnapshot()
			var arg string
			if snap.Running {
				arg = "stop"
			} else {
				arg = "start"
			}
			_ = exec.Command("lerd", arg).Start()
		}()
	}
}

func handleServices(menu *menuState) {
	for i := 0; i < maxServices; i++ {
		go func(idx int) {
			for range menu.svcItems[idx].ClickedCh {
				menu.svcMu.RLock()
				name := menu.svcNames[idx]
				status := menu.svcStatus[idx]
				menu.svcMu.RUnlock()
				if name == "" {
					continue
				}
				arg := "start"
				if status == "active" {
					arg = "stop"
				}
				_ = exec.Command("lerd", "service", arg, name).Start()
			}
		}(i)
	}
}

func handlePHP(menu *menuState) {
	for i := 0; i < maxPHP; i++ {
		go func(idx int) {
			for range menu.phpItems[idx].ClickedCh {
				menu.phpMu.RLock()
				version := menu.phpVersion[idx]
				menu.phpMu.RUnlock()
				if version == "" {
					continue
				}
				_ = exec.Command("lerd", "use", version).Start()
			}
		}(i)
	}
}

func handleAutostart(item *systray.MenuItem) {
	for range item.ClickedCh {
		if lerdSystemd.IsAutostartEnabled() {
			_ = exec.Command("lerd", "autostart", "disable").Start()
		} else {
			_ = exec.Command("lerd", "autostart", "enable").Start()
		}
	}
}

// handleLAN toggles `lerd lan expose` / `lerd lan unexpose` on click. We
// read the current state from config rather than relying on the snapshot
// so the click is robust to a stale in-memory copy.
func handleLAN(item *systray.MenuItem) {
	for range item.ClickedCh {
		exposed := false
		if cfg, err := config.LoadGlobal(); err == nil && cfg != nil {
			exposed = cfg.LAN.Exposed
		}
		if exposed {
			_ = exec.Command("lerd", "lan", "unexpose").Start()
		} else {
			_ = exec.Command("lerd", "lan", "expose").Start()
		}
	}
}

func handleUpdate(item *systray.MenuItem) {
	for range item.ClickedCh {
		info, _ := lerdUpdate.CachedUpdateCheck(version.Version)
		if info != nil {
			openUpdateTerminal(lerdUpdate.StripV(info.LatestVersion))
		} else {
			item.SetTitle("✔ Up to date")
			go func() {
				time.Sleep(3 * time.Second)
				item.SetTitle("Check for update...")
			}()
		}
	}
}

func openUpdateTerminal(latestVer string) {
	script := fmt.Sprintf(
		`echo "Lerd update available: v%s"; `+
			`read -rp "Update now? [y/N] " ans; `+
			`[[ "$ans" =~ ^[Yy]$ ]] && lerd update; `+
			`echo; read -rp "Press Enter to close..."`,
		latestVer,
	)
	terminals := [][]string{
		{"konsole", "-e", "bash", "-c", script},
		{"ptyxis", "--", "bash", "-c", script},
		{"gnome-terminal", "--", "bash", "-c", script},
		{"xfce4-terminal", "-e", "bash -c '" + script + "'"},
		{"xterm", "-e", "bash", "-c", script},
	}
	for _, t := range terminals {
		if _, err := exec.LookPath(t[0]); err == nil {
			_ = exec.Command(t[0], t[1:]...).Start()
			return
		}
	}
}

func handleQuit(item *systray.MenuItem, cancel context.CancelFunc) {
	<-item.ClickedCh
	cancel()
	_ = exec.Command("lerd", "quit").Run()
	systray.Quit()
}

func openURL(url string) {
	_ = exec.Command("xdg-open", url).Start()
}
