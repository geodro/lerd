package cli

import (
	"github.com/geodro/lerd/internal/activityping"
	"github.com/geodro/lerd/internal/config"
)

// recordCwdActivity tells lerd-ui that the site containing dir is being worked
// on, so a CLI/shim command (php, artisan, composer, npm, tinker) keeps the site
// awake under idle-suspend the same way an HTTP request would — and wakes it if
// it was asleep. Best-effort: it pings the lerd-ui unix socket with a tight
// timeout and ignores every failure (lerd-ui not running, dir not in a site).
func recordCwdActivity(dir string) {
	if dir == "" {
		return
	}
	site, _ := config.FindSiteByPath(siteRootFor(dir))
	if site == nil {
		return
	}
	activityping.Site(site.Name)
}
