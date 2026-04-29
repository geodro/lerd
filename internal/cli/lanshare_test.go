package cli

import (
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestShouldRunLANShareProxy(t *testing.T) {
	cases := []struct {
		name string
		site config.Site
		want bool
	}{
		{"no port", config.Site{Name: "a"}, false},
		{"port + active", config.Site{Name: "a", LANPort: 9100}, true},
		{"port + paused", config.Site{Name: "a", LANPort: 9100, Paused: true}, false},
		{"no port + paused", config.Site{Name: "a", Paused: true}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldRunLANShareProxy(c.site); got != c.want {
				t.Errorf("shouldRunLANShareProxy(%+v) = %v, want %v", c.site, got, c.want)
			}
		})
	}
}

func TestRewriteLANShareBody_collapsesHTTPSToHTTP(t *testing.T) {
	in := []byte(`<link href="https://laravel.test/build/app.css">
<script src="https://laravel.test/build/app.js"></script>
<a href="http://laravel.test/login">Login</a>`)

	got := string(rewriteLANShareBody(in, "laravel.test", "192.168.1.42:9100"))

	want := `<link href="http://192.168.1.42:9100/build/app.css">
<script src="http://192.168.1.42:9100/build/app.js"></script>
<a href="http://192.168.1.42:9100/login">Login</a>`

	if got != want {
		t.Errorf("rewriteLANShareBody:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRewriteLANShareBody_downgradesAlreadyRewrittenHTTPS(t *testing.T) {
	// Laravel honors X-Forwarded-Host but forces https from APP_URL, so it
	// already emits https://<lanHost>/asset directly. Must be downgraded.
	in := []byte(`<link href="https://192.168.1.42:9100/build/app.css">
<script src="https://192.168.1.42:9100/build/app.js"></script>`)

	got := string(rewriteLANShareBody(in, "laravel.test", "192.168.1.42:9100"))

	want := `<link href="http://192.168.1.42:9100/build/app.css">
<script src="http://192.168.1.42:9100/build/app.js"></script>`

	if got != want {
		t.Errorf("rewriteLANShareBody downgrade:\nGOT:\n%s\nWANT:\n%s", got, want)
	}
}

func TestRewriteLANShareBody_leavesUnrelatedURLsAlone(t *testing.T) {
	in := []byte(`<img src="https://cdn.example.com/logo.png">
<a href="https://other.test/foo">other</a>`)
	got := rewriteLANShareBody(in, "laravel.test", "192.168.1.42:9100")
	if string(got) != string(in) {
		t.Errorf("expected URLs for other domains untouched:\nGOT:\n%s", got)
	}
}
