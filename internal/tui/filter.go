package tui

import (
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/siteinfo"
)

// siteSortMode picks the ordering for the sites pane.
type siteSortMode int

const (
	siteSortName siteSortMode = iota
	siteSortStatus
	siteSortFramework
)

func (m siteSortMode) label() string {
	switch m {
	case siteSortStatus:
		return "status"
	case siteSortFramework:
		return "framework"
	}
	return "name"
}

// svcSortMode picks the ordering for the services pane.
type svcSortMode int

const (
	svcSortName svcSortMode = iota
	svcSortStatus
	svcSortUsage
)

func (m svcSortMode) label() string {
	switch m {
	case svcSortStatus:
		return "status"
	case svcSortUsage:
		return "usage"
	}
	return "name"
}

// filteredSortedSites returns the sites list after applying the active
// filter and sort. Always returns a new slice so the caller can safely
// mutate ordering without affecting m.snap.
func filteredSortedSites(list []siteinfo.EnrichedSite, filter string, mode siteSortMode) []siteinfo.EnrichedSite {
	out := make([]siteinfo.EnrichedSite, 0, len(list))
	needle := strings.ToLower(strings.TrimSpace(filter))
	for _, s := range list {
		if needle != "" && !siteMatchesFilter(s, needle) {
			continue
		}
		out = append(out, s)
	}
	switch mode {
	case siteSortStatus:
		sort.SliceStable(out, func(i, j int) bool {
			return siteStatusRank(out[i]) < siteStatusRank(out[j])
		})
	case siteSortFramework:
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].FrameworkLabel != out[j].FrameworkLabel {
				return out[i].FrameworkLabel < out[j].FrameworkLabel
			}
			return out[i].Name < out[j].Name
		})
	default:
		sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	}
	return out
}

func siteMatchesFilter(s siteinfo.EnrichedSite, needle string) bool {
	if strings.Contains(strings.ToLower(s.Name), needle) {
		return true
	}
	for _, d := range s.Domains {
		if strings.Contains(strings.ToLower(d), needle) {
			return true
		}
	}
	if strings.Contains(strings.ToLower(s.FrameworkLabel), needle) {
		return true
	}
	return false
}

// siteStatusRank sorts running sites first, then stopped, then paused.
// Within a bucket the sort is stable, so the caller's upstream ordering
// (alphabetical) determines ties.
func siteStatusRank(s siteinfo.EnrichedSite) int {
	switch {
	case s.Paused:
		return 2
	case s.FPMRunning:
		return 0
	}
	return 1
}

// filteredSortedServices applies the active filter/sort to the services row
// slice. Same policy as sites: new slice, stable ordering.
func filteredSortedServices(list []ServiceRow, filter string, mode svcSortMode) []ServiceRow {
	out := make([]ServiceRow, 0, len(list))
	needle := strings.ToLower(strings.TrimSpace(filter))
	for _, s := range list {
		if needle != "" && !strings.Contains(strings.ToLower(s.Name), needle) {
			continue
		}
		out = append(out, s)
	}
	switch mode {
	case svcSortStatus:
		sort.SliceStable(out, func(i, j int) bool {
			return svcStatusRank(out[i]) < svcStatusRank(out[j])
		})
	case svcSortUsage:
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].SiteCount != out[j].SiteCount {
				return out[i].SiteCount > out[j].SiteCount
			}
			return out[i].Name < out[j].Name
		})
	default:
		sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	}
	return out
}

func svcStatusRank(s ServiceRow) int {
	switch s.State {
	case stateRunning:
		return 0
	case statePaused:
		return 2
	}
	return 1
}
