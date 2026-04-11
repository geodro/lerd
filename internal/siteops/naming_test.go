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
