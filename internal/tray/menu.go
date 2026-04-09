//go:build !nogui

package tray

import (
	"fmt"
	"sync"

	"github.com/getlantern/systray"
)

const (
	maxServices = 20
	maxPHP      = 8
)

type menuState struct {
	mStatus *systray.MenuItem
	mNginx  *systray.MenuItem
	mDNS    *systray.MenuItem

	mDash   *systray.MenuItem
	mToggle *systray.MenuItem

	mSvcsHdr  *systray.MenuItem
	svcItems  [maxServices]*systray.MenuItem
	svcNames  [maxServices]string
	svcStatus [maxServices]string // "active" | "inactive" | "failed"
	svcPaused [maxServices]bool
	svcMu     sync.RWMutex

	mPHPHdr    *systray.MenuItem
	phpItems   [maxPHP]*systray.MenuItem
	phpVersion [maxPHP]string
	phpMu      sync.RWMutex

	mAutostart *systray.MenuItem
	mLAN       *systray.MenuItem
	mUpdate    *systray.MenuItem
	mQuit      *systray.MenuItem
}

func buildMenu() *menuState {
	m := &menuState{}

	m.mStatus = systray.AddMenuItem("⏳ Checking...", "")
	m.mStatus.Disable()
	m.mNginx = systray.AddMenuItem("  🔴 nginx", "")
	m.mNginx.Disable()
	m.mDNS = systray.AddMenuItem("  🔴 dns", "")
	m.mDNS.Disable()

	systray.AddSeparator()

	m.mDash = systray.AddMenuItem("Open Dashboard", "Open the Lerd web dashboard")
	m.mToggle = systray.AddMenuItem("Start Lerd", "Start or stop the Lerd environment")

	systray.AddSeparator()

	m.mSvcsHdr = systray.AddMenuItem("── Services ──", "")
	m.mSvcsHdr.Disable()
	for i := range m.svcItems {
		m.svcItems[i] = systray.AddMenuItem("", "")
		m.svcItems[i].Hide()
	}

	systray.AddSeparator()

	m.mPHPHdr = systray.AddMenuItem("── PHP ──", "")
	m.mPHPHdr.Disable()
	for i := range m.phpItems {
		m.phpItems[i] = systray.AddMenuItem("", "")
		m.phpItems[i].Hide()
	}

	systray.AddSeparator()

	m.mAutostart = systray.AddMenuItem("Autostart at login: Off", "Toggle lerd autostart on login")
	m.mLAN = systray.AddMenuItem("Expose to LAN: Off", "Toggle whether lerd is reachable from other devices on the local network")
	m.mUpdate = systray.AddMenuItem("Check for update...", "Check for a newer version of Lerd")
	m.mQuit = systray.AddMenuItem("Quit Lerd", "Stop all Lerd processes and containers")

	return m
}

// apply updates menu titles and visibility from a Snapshot.
func (m *menuState) apply(snap *Snapshot) {
	if snap == nil || !snap.Running {
		m.mStatus.SetTitle("🔴 Stopped")
		m.mToggle.SetTitle("Start Lerd")
	} else {
		m.mStatus.SetTitle("🟢 Running")
		m.mToggle.SetTitle("Stop Lerd")
	}

	// Core services
	nginxDot := "🔴"
	if snap.NginxRunning {
		nginxDot = "🟢"
	}
	m.mNginx.SetTitle(fmt.Sprintf("  %s nginx", nginxDot))

	dnsDot := "🔴"
	if snap.DNSOK {
		dnsDot = "🟢"
	}
	m.mDNS.SetTitle(fmt.Sprintf("  %s dns", dnsDot))

	// Services
	scount := len(snap.Services)
	if scount > maxServices {
		scount = maxServices
	}
	m.svcMu.Lock()
	for i := 0; i < scount; i++ {
		svc := snap.Services[i]
		m.svcNames[i] = svc.Name
		m.svcStatus[i] = svc.Status
		m.svcPaused[i] = svc.Paused
		// Paused services render yellow regardless of unit state — they
		// were manually stopped by the user so "red = broken" would be
		// misleading.
		dot := "🔴"
		switch {
		case svc.Paused:
			dot = "🟡"
		case svc.Status == "active":
			dot = "🟢"
		}
		m.svcItems[i].SetTitle(fmt.Sprintf("%s %s", dot, svc.Name))
		m.svcItems[i].Show()
	}
	for i := scount; i < maxServices; i++ {
		m.svcNames[i] = ""
		m.svcStatus[i] = ""
		m.svcPaused[i] = false
		m.svcItems[i].Hide()
	}
	m.svcMu.Unlock()

	// PHP versions
	pcount := len(snap.PHPVersions)
	if pcount > maxPHP {
		pcount = maxPHP
	}
	m.phpMu.Lock()
	for i := 0; i < pcount; i++ {
		p := snap.PHPVersions[i]
		m.phpVersion[i] = p.Version
		label := p.Version
		if p.Version == snap.PHPDefault {
			label = "✔ " + p.Version
		}
		m.phpItems[i].SetTitle(label)
		m.phpItems[i].Show()
	}
	for i := pcount; i < maxPHP; i++ {
		m.phpVersion[i] = ""
		m.phpItems[i].Hide()
	}
	m.phpMu.Unlock()

	// Autostart
	if snap.AutostartEnabled {
		m.mAutostart.SetTitle("Autostart at login: ✔ On")
	} else {
		m.mAutostart.SetTitle("Autostart at login: Off")
	}

	// LAN exposure
	if snap.LANExposed {
		m.mLAN.SetTitle("Expose to LAN: ✔ On")
	} else {
		m.mLAN.SetTitle("Expose to LAN: Off")
	}

	// Update availability
	if snap.LatestVersion != "" {
		m.mUpdate.SetTitle(fmt.Sprintf("⬆ Update to %s", snap.LatestVersion))
	} else {
		m.mUpdate.SetTitle("Check for update...")
	}
}
