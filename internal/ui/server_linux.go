//go:build linux

package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

func listActiveUnitsBySuffix(pattern, prefix string) []string {
	out, err := exec.Command("systemctl", "--user", "list-units", "--state=active",
		"--no-legend", "--plain", pattern).Output()
	if err != nil {
		return nil
	}
	var sites []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		unit := strings.TrimSuffix(fields[0], ".service")
		siteName := strings.TrimPrefix(unit, prefix)
		if siteName != unit && siteName != "" {
			sites = append(sites, siteName)
		}
	}
	return sites
}

// listActiveStripeListeners returns the site names of active lerd-stripe-* units
// that were started by `lerd stripe:listen` (i.e. have a .service file in the
// systemd user dir, as opposed to quadlet-based services like stripe-mock).
func listActiveStripeListeners() []string {
	all := listActiveUnitsBySuffix("lerd-stripe-*.service", "lerd-stripe-")
	var result []string
	for _, name := range all {
		unitFile := filepath.Join(config.SystemdUserDir(), "lerd-stripe-"+name+".service")
		if _, err := os.Stat(unitFile); err == nil {
			result = append(result, name)
		}
	}
	return result
}
