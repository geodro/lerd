package cli

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestSiteServedByPHPFPM(t *testing.T) {
	cases := []struct {
		name string
		site *config.Site
		want bool
	}{
		{"nil site (bare-linked PHP)", nil, true},
		{"plain PHP site", &config.Site{}, true},
		{"custom-FPM site has a per-site FPM container", &config.Site{Runtime: "fpm-custom"}, true},
		{"frankenphp site", &config.Site{Runtime: "frankenphp"}, true},
		{"host-proxy site runs on the host", &config.Site{HostPort: 3000}, false},
		{"custom-container (non-PHP) site", &config.Site{ContainerPort: 8080}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := siteServedByPHPFPM(c.site); got != c.want {
				t.Errorf("siteServedByPHPFPM() = %v, want %v", got, c.want)
			}
		})
	}
}
