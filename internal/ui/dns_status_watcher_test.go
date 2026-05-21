package ui

import (
	"testing"

	"github.com/geodro/lerd/internal/dns"
)

func resetDNSObs() {
	lastDNSObs.Store(dnsObsUnknown)
	pendingDNSObs.Store(dnsObsUnknown)
}

// TestTickDNSStatus pins the three-way observation and the two-tick
// debounce: a change publishes only after it survives two consecutive
// ticks, so a single transient blip (common the moment a VPN connects)
// never flips the dashboard pill.
func TestTickDNSStatus(t *testing.T) {
	cases := []struct {
		name        string
		visible     bool
		start       int32
		probes      []dns.Status
		wantObs     int32
		wantPublish int
	}{
		{
			name:    "skipped when no tab is visible",
			visible: false,
			start:   dnsObsUnknown,
			probes:  []dns.Status{dns.StatusOK},
			wantObs: dnsObsUnknown,
		},
		{
			name:        "first ok observation latches and publishes",
			visible:     true,
			start:       dnsObsUnknown,
			probes:      []dns.Status{dns.StatusOK},
			wantObs:     dnsObsOK,
			wantPublish: 1,
		},
		{
			name:        "first down observation latches and publishes",
			visible:     true,
			start:       dnsObsUnknown,
			probes:      []dns.Status{dns.StatusDown},
			wantObs:     dnsObsDown,
			wantPublish: 1,
		},
		{
			name:    "steady ok is silent",
			visible: true,
			start:   dnsObsOK,
			probes:  []dns.Status{dns.StatusOK, dns.StatusOK},
			wantObs: dnsObsOK,
		},
		{
			name:    "single blip does not latch",
			visible: true,
			start:   dnsObsOK,
			probes:  []dns.Status{dns.StatusDown, dns.StatusOK},
			wantObs: dnsObsOK,
		},
		{
			name:        "change confirmed on second consecutive tick",
			visible:     true,
			start:       dnsObsOK,
			probes:      []dns.Status{dns.StatusDown, dns.StatusDown},
			wantObs:     dnsObsDown,
			wantPublish: 1,
		},
		{
			name:        "ok to degraded publishes after debounce",
			visible:     true,
			start:       dnsObsOK,
			probes:      []dns.Status{dns.StatusDegraded, dns.StatusDegraded},
			wantObs:     dnsObsDegraded,
			wantPublish: 1,
		},
		{
			name:        "degraded recovery to ok publishes after debounce",
			visible:     true,
			start:       dnsObsDegraded,
			probes:      []dns.Status{dns.StatusOK, dns.StatusOK},
			wantObs:     dnsObsOK,
			wantPublish: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lastDNSObs.Store(tc.start)
			pendingDNSObs.Store(tc.start)
			t.Cleanup(resetDNSObs)

			published := 0
			i := 0
			deps := dnsStatusDeps{
				tld: func() string { return "test" },
				check: func(string) dns.Status {
					s := tc.probes[i]
					i++
					return s
				},
				visible: func() bool { return tc.visible },
				publish: func() { published++ },
			}

			for range tc.probes {
				tickDNSStatus(deps)
			}

			if got := lastDNSObs.Load(); got != tc.wantObs {
				t.Fatalf("lastDNSObs = %d, want %d", got, tc.wantObs)
			}
			if published != tc.wantPublish {
				t.Fatalf("publishes = %d, want %d", published, tc.wantPublish)
			}
		})
	}
}

func TestTickDNSStatusTLDFromConfig(t *testing.T) {
	resetDNSObs()
	t.Cleanup(resetDNSObs)

	var seen string
	deps := dnsStatusDeps{
		tld: func() string { return "lerd" },
		check: func(tld string) dns.Status {
			seen = tld
			return dns.StatusOK
		},
		visible: func() bool { return true },
		publish: func() {},
	}
	tickDNSStatus(deps)
	if seen != "lerd" {
		t.Fatalf("check called with tld=%q, want %q", seen, "lerd")
	}
}
