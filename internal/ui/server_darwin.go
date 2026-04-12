//go:build darwin

package ui

import (
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/services"
)

// listActiveUnitsBySuffix returns site names for active units with the given prefix.
// On macOS, workers run as containers; we list units via the service manager and
// check active status (which covers both containers and launchd-managed services).
func listActiveUnitsBySuffix(_, prefix string) []string {
	// Strip trailing dash from prefix to form the glob, e.g. "lerd-queue-" → "lerd-queue-*"
	nameGlob := strings.TrimSuffix(prefix, "-") + "-*"
	units := services.Mgr.ListContainerUnits(nameGlob)
	var sites []string
	for _, unit := range units {
		if !services.Mgr.IsActive(unit) {
			continue
		}
		siteName := strings.TrimPrefix(unit, prefix)
		if siteName != unit && siteName != "" {
			sites = append(sites, siteName)
		}
	}
	return sites
}

// listActiveStripeListeners returns site names of active lerd-stripe-* units
// that correspond to registered sites (excludes presets like stripe-mock).
func listActiveStripeListeners() []string {
	all := listActiveUnitsBySuffix("", "lerd-stripe-")
	reg, err := config.LoadSites()
	if err != nil {
		return all
	}
	siteNames := make(map[string]bool, len(reg.Sites))
	for _, s := range reg.Sites {
		siteNames[s.Name] = true
	}
	var result []string
	for _, name := range all {
		if siteNames[name] {
			result = append(result, name)
		}
	}
	return result
}
