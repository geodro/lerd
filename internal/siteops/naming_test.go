package siteops

import "testing"

func TestSiteNameAndDomain(t *testing.T) {
	cases := []struct {
		dirName    string
		tld        string
		wantName   string
		wantDomain string
	}{
		{"myapp", "test", "myapp", "myapp.test"},
		{"myapp.com", "test", "myapp", "myapp.test"},
		{"my-app.io", "test", "my-app", "my-app.test"},
		{"My-App.COM", "test", "my-app", "my-app.test"},
		{"foo.bar.baz", "test", "foo-bar-baz", "foo-bar-baz.test"},
		{"example.co.uk", "test", "example-co", "example-co.test"}, // .uk stripped first
		{"plain", "local", "plain", "plain.local"},
		{"dots.in.name", "test", "dots-in-name", "dots-in-name.test"},

		// ccTLDs handled by the 2-letter regex, no enumeration needed.
		{"astrolov.ro", "test", "astrolov", "astrolov.test"},
		{"mysite.nl", "test", "mysite", "mysite.test"},
		{"mysite.be", "test", "mysite", "mysite.test"},
		{"project.pl", "test", "project", "project.test"},

		// gTLDs from the curated list.
		{"shop.online", "test", "shop", "shop.test"},
		{"studio.digital", "test", "studio", "studio.test"},

		// Digit suffix must not be stripped; preserves version-style dir names.
		{"app.v2", "test", "app-v2", "app-v2.test"},

		// Unknown longer suffix is left alone.
		{"backup.old", "test", "backup-old", "backup-old.test"},
	}

	for _, c := range cases {
		name, domain := SiteNameAndDomain(c.dirName, c.tld)
		if name != c.wantName {
			t.Errorf("SiteNameAndDomain(%q, %q) name = %q, want %q", c.dirName, c.tld, name, c.wantName)
		}
		if domain != c.wantDomain {
			t.Errorf("SiteNameAndDomain(%q, %q) domain = %q, want %q", c.dirName, c.tld, domain, c.wantDomain)
		}
	}
}
