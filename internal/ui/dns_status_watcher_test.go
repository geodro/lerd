package ui

import (
	"testing"
)

func resetDNSObs() { lastDNSOK.Store(dnsObsUnknown) }

func TestTickDNSStatus(t *testing.T) {
	cases := []struct {
		name        string
		visible     bool
		startObs    int32
		probeOK     bool
		wantPublish bool
		wantObs     int32
	}{
		{
			name:        "skipped when no tab is visible",
			visible:     false,
			startObs:    dnsObsUnknown,
			probeOK:     true,
			wantPublish: false,
			wantObs:     dnsObsUnknown,
		},
		{
			name:        "first up observation publishes (boot snapshot was stale-down)",
			visible:     true,
			startObs:    dnsObsUnknown,
			probeOK:     true,
			wantPublish: true,
			wantObs:     dnsObsUp,
		},
		{
			name:        "first down observation publishes",
			visible:     true,
			startObs:    dnsObsUnknown,
			probeOK:     false,
			wantPublish: true,
			wantObs:     dnsObsDown,
		},
		{
			name:        "steady up is silent",
			visible:     true,
			startObs:    dnsObsUp,
			probeOK:     true,
			wantPublish: false,
			wantObs:     dnsObsUp,
		},
		{
			name:        "steady down is silent",
			visible:     true,
			startObs:    dnsObsDown,
			probeOK:     false,
			wantPublish: false,
			wantObs:     dnsObsDown,
		},
		{
			name:        "down to up publishes (post-repair recovery)",
			visible:     true,
			startObs:    dnsObsDown,
			probeOK:     true,
			wantPublish: true,
			wantObs:     dnsObsUp,
		},
		{
			name:        "up to down publishes (resolv.conf got clobbered)",
			visible:     true,
			startObs:    dnsObsUp,
			probeOK:     false,
			wantPublish: true,
			wantObs:     dnsObsDown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lastDNSOK.Store(tc.startObs)
			t.Cleanup(resetDNSObs)

			published := 0
			deps := dnsStatusDeps{
				tld:     func() string { return "test" },
				check:   func(string) (bool, error) { return tc.probeOK, nil },
				visible: func() bool { return tc.visible },
				publish: func() { published++ },
			}

			tickDNSStatus(deps)

			gotObs := lastDNSOK.Load()
			if gotObs != tc.wantObs {
				t.Fatalf("lastDNSOK = %d, want %d", gotObs, tc.wantObs)
			}
			gotPublish := published > 0
			if gotPublish != tc.wantPublish {
				t.Fatalf("publish fired=%v, want %v", gotPublish, tc.wantPublish)
			}
		})
	}
}

func TestTickDNSStatusTLDFromConfig(t *testing.T) {
	resetDNSObs()
	t.Cleanup(resetDNSObs)

	var seen string
	deps := dnsStatusDeps{
		tld:     func() string { return "lerd" },
		check:   func(tld string) (bool, error) { seen = tld; return true, nil },
		visible: func() bool { return true },
		publish: func() {},
	}
	tickDNSStatus(deps)
	if seen != "lerd" {
		t.Fatalf("check called with tld=%q, want %q", seen, "lerd")
	}
}
