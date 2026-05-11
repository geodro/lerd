//go:build darwin

package cli

import "github.com/geodro/lerd/internal/config"

// workerSupportedOnPlatform reports whether the given worker can run on the
// current host. Two shapes lack a macOS path today:
//
//   - host: true → would need a launchd plist that runs through fnm on the
//     host instead of `podman exec` into the FPM container.
//   - schedule != "" → launchd has StartCalendarInterval but the unit
//     translation hasn't been wired through services.Mgr yet.
//
// Returning false here makes WorkerStartForSite skip the lifecycle calls
// entirely instead of writing nothing and then trying to StartUnit on a
// unit that was never written.
var workerSupportedOnPlatform = func(w config.FrameworkWorker) (bool, string) {
	if w.Host {
		return false, "host: true workers aren't supported on macOS yet — run the command manually from the project root"
	}
	if w.Schedule != "" {
		return false, "scheduled workers aren't supported on macOS yet"
	}
	return true, ""
}
