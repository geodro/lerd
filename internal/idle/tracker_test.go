package idle

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadActivity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idle-activity.json")
	tr := NewTracker(nil)
	tr.TouchSite("a", time.Unix(1000, 0))
	tr.TouchSite("b", time.Unix(2000, 0))
	if err := tr.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	m := LoadActivity(path)
	if m["a"] != 1000 || m["b"] != 2000 {
		t.Errorf("loaded %v, want a=1000 b=2000", m)
	}
	if LoadActivity(filepath.Join(t.TempDir(), "missing.json")) != nil {
		t.Error("missing file should load to nil")
	}
}

func staticResolver(m map[string]string) SiteResolver {
	return func(host string) (string, bool) {
		s, ok := m[host]
		return s, ok
	}
}

func TestTouchSite_monotonic(t *testing.T) {
	tr := NewTracker(nil)
	t0 := time.Unix(1000, 0)
	tr.TouchSite("a", t0)
	// An older datagram arriving late must not move the record backwards.
	tr.TouchSite("a", t0.Add(-time.Minute))
	if got, _ := tr.LastActive("a"); !got.Equal(t0) {
		t.Errorf("last active = %v, want %v (no backwards moves)", got, t0)
	}
	// A newer one advances it.
	t1 := t0.Add(time.Minute)
	tr.TouchSite("a", t1)
	if got, _ := tr.LastActive("a"); !got.Equal(t1) {
		t.Errorf("last active = %v, want %v", got, t1)
	}
}

func TestTouchSite_emptyIgnored(t *testing.T) {
	tr := NewTracker(nil)
	tr.TouchSite("", time.Unix(1, 0))
	if len(tr.Snapshot()) != 0 {
		t.Error("empty site name must not create a record")
	}
}

func TestTouchHost_resolvesDomainsToSite(t *testing.T) {
	tr := NewTracker(staticResolver(map[string]string{
		"myapp.test":       "myapp",
		"admin.myapp.test": "myapp", // a second domain of the same site
	}))
	now := time.Unix(2000, 0)
	if got := tr.TouchHost("myapp.test", now); got != "myapp" {
		t.Errorf("TouchHost primary -> %q, want myapp", got)
	}
	if got := tr.TouchHost("admin.myapp.test:443", now.Add(time.Second)); got != "myapp" {
		t.Errorf("TouchHost secondary domain (with port) -> %q, want myapp", got)
	}
	// Both domains collapse onto one site record.
	if len(tr.Snapshot()) != 1 {
		t.Errorf("snapshot has %d records, want 1 (domains collapse to a site)", len(tr.Snapshot()))
	}
}

func TestTouchHost_unmatchedIgnored(t *testing.T) {
	tr := NewTracker(staticResolver(map[string]string{"myapp.test": "myapp"}))
	if got := tr.TouchHost("stranger.test", time.Unix(1, 0)); got != "" {
		t.Errorf("unmatched host -> %q, want empty", got)
	}
	if len(tr.Snapshot()) != 0 {
		t.Error("unmatched host must not create a record")
	}
}

func TestTouchHost_nilResolverNoop(t *testing.T) {
	tr := NewTracker(nil)
	if got := tr.TouchHost("myapp.test", time.Unix(1, 0)); got != "" {
		t.Errorf("nil resolver -> %q, want empty", got)
	}
}

func TestIdleFor_grace(t *testing.T) {
	tr := NewTracker(nil)
	now := time.Unix(5000, 0)
	// No record yet: must report ok=false so the engine applies a grace window
	// instead of treating an unseen site as infinitely idle.
	if _, ok := tr.IdleFor("a", now); ok {
		t.Error("unseen site must report ok=false from IdleFor")
	}
	tr.TouchSite("a", now.Add(-10*time.Minute))
	d, ok := tr.IdleFor("a", now)
	if !ok || d != 10*time.Minute {
		t.Errorf("IdleFor = %v, %v; want 10m, true", d, ok)
	}
}

func TestSeed_doesNotOverwriteNewer(t *testing.T) {
	tr := NewTracker(nil)
	recent := time.Unix(9000, 0)
	tr.TouchSite("a", recent)
	// Seeding at an older time (startup grace) must not pull a fresh record back.
	tr.Seed([]string{"a", "b"}, recent.Add(-time.Hour))
	if got, _ := tr.LastActive("a"); !got.Equal(recent) {
		t.Errorf("seed clobbered newer record: %v, want %v", got, recent)
	}
	if _, ok := tr.LastActive("b"); !ok {
		t.Error("seed must create a record for an unseen site")
	}
}

func TestForget(t *testing.T) {
	tr := NewTracker(nil)
	tr.TouchSite("a", time.Unix(1, 0))
	tr.Forget("a")
	if _, ok := tr.LastActive("a"); ok {
		t.Error("Forget must drop the record")
	}
}

func TestParseAccessHost(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain host", "myapp.test", "myapp.test"},
		{"syslog framed", "<190>Jun 12 10:00:00 lerdaccess: myapp.test", "myapp.test"},
		{"trailing newline", "myapp.test\n", "myapp.test"},
		{"no host header dash", "<190>Jun 12 10:00:00 lerdaccess: -", ""},
		{"empty", "", ""},
		{"whitespace only", "   \n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseAccessHost([]byte(tc.in)); got != tc.want {
				t.Errorf("ParseAccessHost(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"Myapp.test":     "myapp.test",
		"myapp.test:443": "myapp.test",
		"  myapp.test ":  "myapp.test",
	}
	for in, want := range cases {
		if got := normalizeHost(in); got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}
