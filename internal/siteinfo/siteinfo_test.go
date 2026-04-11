package siteinfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func setDataDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// stubPodman replaces podman functions with no-ops for testing and restores
// them when the test finishes.
func stubPodman(t *testing.T) {
	t.Helper()
	origUnit := unitStatusFn
	origContainer := containerRunningFn
	unitStatusFn = func(string) (string, error) { return "", nil }
	containerRunningFn = func(string) (bool, error) { return false, nil }
	t.Cleanup(func() {
		unitStatusFn = origUnit
		containerRunningFn = origContainer
	})
}

// ── DetectFavicon ───────────────────────────────────────────────────────────

func TestDetectFavicon(t *testing.T) {
	t.Run("finds favicon.ico in public dir", func(t *testing.T) {
		dir := t.TempDir()
		pub := filepath.Join(dir, "public")
		os.MkdirAll(pub, 0755)
		os.WriteFile(filepath.Join(pub, "index.php"), []byte("<?php"), 0644)
		os.WriteFile(filepath.Join(pub, "favicon.ico"), []byte("icon"), 0644)

		got := DetectFavicon(dir, "public", "", nil, false)
		if got != filepath.Join(pub, "favicon.ico") {
			t.Errorf("got %q, want %q", got, filepath.Join(pub, "favicon.ico"))
		}
	})

	t.Run("finds favicon.svg over ico when ico missing", func(t *testing.T) {
		dir := t.TempDir()
		pub := filepath.Join(dir, "public")
		os.MkdirAll(pub, 0755)
		os.WriteFile(filepath.Join(pub, "favicon.svg"), []byte("<svg/>"), 0644)

		got := DetectFavicon(dir, "public", "", nil, false)
		if got != filepath.Join(pub, "favicon.svg") {
			t.Errorf("got %q, want %q", got, filepath.Join(pub, "favicon.svg"))
		}
	})

	t.Run("prefers ico over svg", func(t *testing.T) {
		dir := t.TempDir()
		pub := filepath.Join(dir, "public")
		os.MkdirAll(pub, 0755)
		os.WriteFile(filepath.Join(pub, "favicon.ico"), []byte("icon"), 0644)
		os.WriteFile(filepath.Join(pub, "favicon.svg"), []byte("<svg/>"), 0644)

		got := DetectFavicon(dir, "public", "", nil, false)
		if got != filepath.Join(pub, "favicon.ico") {
			t.Errorf("got %q, want %q", got, filepath.Join(pub, "favicon.ico"))
		}
	})

	t.Run("returns empty when no favicon exists", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "public"), 0755)

		got := DetectFavicon(dir, "public", "", nil, false)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("uses project root when publicDir is dot", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "favicon.png"), []byte("png"), 0644)

		got := DetectFavicon(dir, ".", "", nil, false)
		if got != filepath.Join(dir, "favicon.png") {
			t.Errorf("got %q, want %q", got, filepath.Join(dir, "favicon.png"))
		}
	})

	t.Run("skips empty favicon file", func(t *testing.T) {
		dir := t.TempDir()
		pub := filepath.Join(dir, "public")
		os.MkdirAll(pub, 0755)
		os.WriteFile(filepath.Join(pub, "favicon.ico"), []byte{}, 0644)

		got := DetectFavicon(dir, "public", "", nil, false)
		if got != "" {
			t.Errorf("got %q, want empty for 0-byte favicon", got)
		}
	})

	t.Run("auto-detects public dir when empty", func(t *testing.T) {
		dir := t.TempDir()
		pub := filepath.Join(dir, "public")
		os.MkdirAll(pub, 0755)
		os.WriteFile(filepath.Join(pub, "index.php"), []byte("<?php"), 0644)
		os.WriteFile(filepath.Join(pub, "favicon.ico"), []byte("icon"), 0644)

		got := DetectFavicon(dir, "", "", nil, false)
		if got != filepath.Join(pub, "favicon.ico") {
			t.Errorf("got %q, want %q", got, filepath.Join(pub, "favicon.ico"))
		}
	})

	t.Run("uses framework favicon path", func(t *testing.T) {
		dir := t.TempDir()
		pub := filepath.Join(dir, "public")
		os.MkdirAll(filepath.Join(pub, "core", "misc"), 0755)
		os.WriteFile(filepath.Join(pub, "core", "misc", "favicon.ico"), []byte("drupal"), 0644)

		fw := &config.Framework{Favicon: "core/misc/favicon.ico"}
		got := DetectFavicon(dir, "public", "", fw, true)
		if got != filepath.Join(pub, "core", "misc", "favicon.ico") {
			t.Errorf("got %q, want framework favicon path", got)
		}
	})
}

// ── FrameworkLabel ──────────────────────────────────────────────────────────

func TestFrameworkLabelInternal(t *testing.T) {
	t.Run("empty name returns empty", func(t *testing.T) {
		got := frameworkLabel("", "/tmp", nil, false)
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("no framework found returns raw name", func(t *testing.T) {
		got := frameworkLabel("unknown-fw", "/tmp", nil, false)
		if got != "unknown-fw" {
			t.Errorf("got %q, want %q", got, "unknown-fw")
		}
	})

	t.Run("framework with label only", func(t *testing.T) {
		fw := &config.Framework{Label: "Laravel"}
		got := frameworkLabel("laravel", "/tmp", fw, true)
		if got != "Laravel" {
			t.Errorf("got %q, want %q", got, "Laravel")
		}
	})

	t.Run("framework with label and version", func(t *testing.T) {
		fw := &config.Framework{Label: "Laravel", Version: "11"}
		got := frameworkLabel("laravel", "/tmp", fw, true)
		if got != "Laravel 11" {
			t.Errorf("got %q, want %q", got, "Laravel 11")
		}
	})
}

// ── HasLogFiles ─────────────────────────────────────────────────────────────

func TestHasLogFiles(t *testing.T) {
	t.Run("no framework returns false", func(t *testing.T) {
		if hasLogFiles(false, nil, "/tmp") {
			t.Error("expected false when no framework")
		}
	})

	t.Run("framework without logs returns false", func(t *testing.T) {
		fw := &config.Framework{}
		if hasLogFiles(true, fw, "/tmp") {
			t.Error("expected false when no log sources defined")
		}
	})

	t.Run("finds matching log files", func(t *testing.T) {
		dir := t.TempDir()
		logDir := filepath.Join(dir, "storage", "logs")
		os.MkdirAll(logDir, 0755)
		os.WriteFile(filepath.Join(logDir, "laravel.log"), []byte("log"), 0644)

		fw := &config.Framework{
			Logs: []config.FrameworkLogSource{{Path: "storage/logs/*.log"}},
		}
		if !hasLogFiles(true, fw, dir) {
			t.Error("expected true when log files exist")
		}
	})

	t.Run("no matching log files", func(t *testing.T) {
		dir := t.TempDir()
		fw := &config.Framework{
			Logs: []config.FrameworkLogSource{{Path: "storage/logs/*.log"}},
		}
		if hasLogFiles(true, fw, dir) {
			t.Error("expected false when no log files match")
		}
	})
}

// ── LatestLogTime ───────────────────────────────────────────────────────────

func TestLatestLogTime(t *testing.T) {
	t.Run("no framework returns empty", func(t *testing.T) {
		got := latestLogTime(false, nil, "/tmp")
		if got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("returns timestamp for existing logs", func(t *testing.T) {
		dir := t.TempDir()
		logDir := filepath.Join(dir, "storage", "logs")
		os.MkdirAll(logDir, 0755)
		os.WriteFile(filepath.Join(logDir, "laravel.log"), []byte("log entry"), 0644)

		fw := &config.Framework{
			Logs: []config.FrameworkLogSource{{Path: "storage/logs/*.log"}},
		}
		got := latestLogTime(true, fw, dir)
		if got == "" {
			t.Error("expected non-empty timestamp")
		}
	})
}

// ── Service auto-detection ──────────────────────────────────────────────────

func TestEnrichServices(t *testing.T) {
	t.Run("detects services from .env", func(t *testing.T) {
		dir := t.TempDir()
		envContent := "DB_HOST=lerd-mysql\nCACHE_STORE=lerd-redis\n"
		os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644)

		e := &EnrichedSite{Path: dir}
		e.enrichServices()

		svcMap := make(map[string]bool)
		for _, s := range e.Services {
			svcMap[s] = true
		}
		if !svcMap["mysql"] {
			t.Error("expected mysql to be detected")
		}
		if !svcMap["redis"] {
			t.Error("expected redis to be detected")
		}
		if svcMap["postgres"] {
			t.Error("expected postgres to NOT be detected")
		}
	})

	t.Run("no .env file returns empty", func(t *testing.T) {
		dir := t.TempDir()
		e := &EnrichedSite{Path: dir}
		e.enrichServices()

		if len(e.Services) != 0 {
			t.Errorf("expected no services, got %v", e.Services)
		}
	})
}

// ── Node version filtering ──────────────────────────────────────────────────

func TestNodeVersionFiltering(t *testing.T) {
	t.Run("non-numeric values are discarded", func(t *testing.T) {
		e := EnrichedSite{NodeVersion: "system"}
		// Simulate what enrichVersions does for filtering
		if v := e.NodeVersion; v != "" {
			if len(v) > 0 {
				for _, c := range v {
					if c < '0' || c > '9' {
						e.NodeVersion = ""
						break
					}
				}
			}
		}
		if e.NodeVersion != "" {
			t.Errorf("got %q, want empty for non-numeric version", e.NodeVersion)
		}
	})

	t.Run("numeric values are kept", func(t *testing.T) {
		e := EnrichedSite{NodeVersion: "22"}
		if v := e.NodeVersion; v != "" {
			for _, c := range v {
				if c < '0' || c > '9' {
					e.NodeVersion = ""
					break
				}
			}
		}
		if e.NodeVersion != "22" {
			t.Errorf("got %q, want %q", e.NodeVersion, "22")
		}
	})
}

// ── Ignored sites filtering ─────────────────────────────────────────────────

func TestIgnoredSitesSkipped(t *testing.T) {
	setDataDir(t)
	stubPodman(t)

	config.AddSite(config.Site{Name: "active", Domains: []string{"active.test"}, Path: t.TempDir()})
	config.AddSite(config.Site{Name: "ignored", Domains: []string{"ignored.test"}, Path: t.TempDir()})
	config.IgnoreSite("ignored")

	sites, err := LoadAll(0)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	for _, s := range sites {
		if s.Name == "ignored" {
			t.Error("ignored site should not appear in LoadAll results")
		}
	}
	if len(sites) != 1 {
		t.Errorf("expected 1 site, got %d", len(sites))
	}
}

// ── EnrichFlag controls ─────────────────────────────────────────────────────

func TestEnrichFlags(t *testing.T) {
	stubPodman(t)

	dir := t.TempDir()
	site := config.Site{
		Name:    "myapp",
		Domains: []string{"myapp.test"},
		Path:    dir,
	}

	t.Run("zero flags populates only base fields", func(t *testing.T) {
		e := Enrich(site, 0)
		if e.Name != "myapp" {
			t.Errorf("Name = %q, want myapp", e.Name)
		}
		if e.FrameworkLabel != "" {
			t.Errorf("FrameworkLabel should be empty with no flags, got %q", e.FrameworkLabel)
		}
		if e.Branch != "" {
			t.Errorf("Branch should be empty with no EnrichGit flag, got %q", e.Branch)
		}
	})

	t.Run("EnrichGit populates branch", func(t *testing.T) {
		// Create a minimal git repo to test
		gitDir := filepath.Join(dir, ".git")
		os.MkdirAll(gitDir, 0755)
		os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644)

		e := Enrich(site, EnrichGit)
		if e.Branch != "main" {
			t.Errorf("Branch = %q, want main", e.Branch)
		}
	})

	t.Run("EnrichFavicon populates HasFavicon", func(t *testing.T) {
		pub := filepath.Join(dir, "public")
		os.MkdirAll(pub, 0755)
		os.WriteFile(filepath.Join(pub, "index.php"), []byte("<?php"), 0644)
		os.WriteFile(filepath.Join(pub, "favicon.ico"), []byte("icon"), 0644)

		e := Enrich(site, EnrichFavicon)
		if !e.HasFavicon {
			t.Error("expected HasFavicon = true")
		}
	})
}

// ── LoadAll basic ───────────────────────────────────────────────────────────

func TestLoadAll_Empty(t *testing.T) {
	setDataDir(t)
	stubPodman(t)

	sites, err := LoadAll(0)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(sites) != 0 {
		t.Errorf("expected 0 sites, got %d", len(sites))
	}
}

func TestLoadAll_PopulatesBaseFields(t *testing.T) {
	setDataDir(t)
	stubPodman(t)

	dir := t.TempDir()
	config.AddSite(config.Site{
		Name:       "myapp",
		Domains:    []string{"myapp.test", "api.test"},
		Path:       dir,
		PHPVersion: "8.4",
		Secured:    true,
	})

	sites, err := LoadAll(0)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}

	s := sites[0]
	if s.Name != "myapp" {
		t.Errorf("Name = %q, want myapp", s.Name)
	}
	if s.PrimaryDomain() != "myapp.test" {
		t.Errorf("PrimaryDomain() = %q, want myapp.test", s.PrimaryDomain())
	}
	if len(s.Domains) != 2 {
		t.Errorf("expected 2 domains, got %d", len(s.Domains))
	}
	if s.PHPVersion != "8.4" {
		t.Errorf("PHPVersion = %q, want 8.4", s.PHPVersion)
	}
	if !s.Secured {
		t.Error("expected Secured = true")
	}
}

// ── PrimaryDomain ───────────────────────────────────────────────────────────

func TestEnrichedSitePrimaryDomain(t *testing.T) {
	e := EnrichedSite{Domains: []string{"a.test", "b.test"}}
	if got := e.PrimaryDomain(); got != "a.test" {
		t.Errorf("PrimaryDomain() = %q, want a.test", got)
	}

	e2 := EnrichedSite{}
	if got := e2.PrimaryDomain(); got != "" {
		t.Errorf("PrimaryDomain() = %q, want empty", got)
	}
}

// ── Stripe detection ────────────────────────────────────────────────────────

func TestEnrichStripe(t *testing.T) {
	t.Run("no .env means no stripe", func(t *testing.T) {
		e := &EnrichedSite{Path: t.TempDir(), Name: "myapp"}
		e.enrichStripe()
		if e.StripeSecretSet {
			t.Error("expected StripeSecretSet = false")
		}
	})

	t.Run("STRIPE_SECRET in .env sets flag", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".env"), []byte("STRIPE_SECRET=sk_test_123\n"), 0644)

		origUnit := unitStatusFn
		unitStatusFn = func(name string) (string, error) {
			if name == "lerd-stripe-myapp" {
				return "active", nil
			}
			return "", nil
		}
		defer func() { unitStatusFn = origUnit }()

		e := &EnrichedSite{Path: dir, Name: "myapp"}
		e.enrichStripe()
		if !e.StripeSecretSet {
			t.Error("expected StripeSecretSet = true")
		}
		if !e.StripeRunning {
			t.Error("expected StripeRunning = true")
		}
	})
}

// ── Worker enrichment ───────────────────────────────────────────────────────

func TestEnrichWorkers(t *testing.T) {
	t.Run("no framework means no workers", func(t *testing.T) {
		e := &EnrichedSite{Name: "myapp", Path: t.TempDir()}
		e.enrichWorkers(nil, false)
		if e.HasQueueWorker || e.HasScheduleWorker || e.HasReverb || e.HasHorizon {
			t.Error("expected no workers without framework")
		}
	})

	t.Run("queue worker detected and running", func(t *testing.T) {
		origUnit := unitStatusFn
		unitStatusFn = func(name string) (string, error) {
			if name == "lerd-queue-myapp" {
				return "active", nil
			}
			return "", nil
		}
		defer func() { unitStatusFn = origUnit }()

		dir := t.TempDir()
		fw := &config.Framework{
			Workers: map[string]config.FrameworkWorker{
				"queue": {Command: "php artisan queue:work"},
			},
		}
		e := &EnrichedSite{Name: "myapp", Path: dir}
		e.enrichWorkers(fw, true)

		if !e.HasQueueWorker {
			t.Error("expected HasQueueWorker = true")
		}
		if !e.QueueRunning {
			t.Error("expected QueueRunning = true")
		}
	})

	t.Run("horizon suppresses queue", func(t *testing.T) {
		origUnit := unitStatusFn
		unitStatusFn = func(name string) (string, error) {
			if name == "lerd-horizon-myapp" {
				return "active", nil
			}
			return "", nil
		}
		defer func() { unitStatusFn = origUnit }()

		dir := t.TempDir()
		fw := &config.Framework{
			Workers: map[string]config.FrameworkWorker{
				"queue":   {Command: "php artisan queue:work"},
				"horizon": {Command: "php artisan horizon"},
			},
		}
		e := &EnrichedSite{Name: "myapp", Path: dir}
		e.enrichWorkers(fw, true)

		if e.HasQueueWorker {
			t.Error("expected HasQueueWorker = false when horizon is present")
		}
		if !e.HasHorizon {
			t.Error("expected HasHorizon = true")
		}
	})

	t.Run("failing worker detected", func(t *testing.T) {
		origUnit := unitStatusFn
		unitStatusFn = func(name string) (string, error) {
			if name == "lerd-queue-myapp" {
				return "failed", nil
			}
			return "", nil
		}
		defer func() { unitStatusFn = origUnit }()

		dir := t.TempDir()
		fw := &config.Framework{
			Workers: map[string]config.FrameworkWorker{
				"queue": {Command: "php artisan queue:work"},
			},
		}
		e := &EnrichedSite{Name: "myapp", Path: dir}
		e.enrichWorkers(fw, true)

		if !e.QueueFailing {
			t.Error("expected QueueFailing = true")
		}
		if e.QueueRunning {
			t.Error("expected QueueRunning = false")
		}
	})

	t.Run("custom workers sorted alphabetically", func(t *testing.T) {
		origUnit := unitStatusFn
		unitStatusFn = func(string) (string, error) { return "", nil }
		defer func() { unitStatusFn = origUnit }()

		dir := t.TempDir()
		fw := &config.Framework{
			Workers: map[string]config.FrameworkWorker{
				"zebra":  {Command: "zebra-cmd", Label: "Zebra Worker"},
				"alpha":  {Command: "alpha-cmd", Label: "Alpha Worker"},
				"middle": {Command: "mid-cmd"},
			},
		}
		e := &EnrichedSite{Name: "myapp", Path: dir}
		e.enrichWorkers(fw, true)

		if len(e.FrameworkWorkers) != 3 {
			t.Fatalf("expected 3 custom workers, got %d", len(e.FrameworkWorkers))
		}
		if e.FrameworkWorkers[0].Name != "alpha" {
			t.Errorf("first worker = %q, want alpha", e.FrameworkWorkers[0].Name)
		}
		if e.FrameworkWorkers[1].Name != "middle" {
			t.Errorf("second worker = %q, want middle", e.FrameworkWorkers[1].Name)
		}
		if e.FrameworkWorkers[1].Label != "middle" {
			t.Errorf("worker without Label should use name, got %q", e.FrameworkWorkers[1].Label)
		}
		if e.FrameworkWorkers[2].Name != "zebra" {
			t.Errorf("third worker = %q, want zebra", e.FrameworkWorkers[2].Name)
		}
	})

	t.Run("conflicts_with suppresses workers", func(t *testing.T) {
		origUnit := unitStatusFn
		unitStatusFn = func(name string) (string, error) {
			if name == "lerd-horizon-myapp" {
				return "active", nil
			}
			return "", nil
		}
		defer func() { unitStatusFn = origUnit }()

		dir := t.TempDir()
		fw := &config.Framework{
			Workers: map[string]config.FrameworkWorker{
				"queue":   {Command: "php artisan queue:work"},
				"horizon": {Command: "php artisan horizon", ConflictsWith: []string{"queue"}},
			},
		}
		e := &EnrichedSite{Name: "myapp", Path: dir}
		e.enrichWorkers(fw, true)

		if e.HasQueueWorker {
			t.Error("queue should be suppressed by running horizon's ConflictsWith")
		}
	})
}

// ── FPM enrichment ──────────────────────────────────────────────────────────

func TestEnrichFPM(t *testing.T) {
	t.Run("no PHP version means no FPM check", func(t *testing.T) {
		e := &EnrichedSite{PHPVersion: ""}
		e.enrichFPM()
		if e.FPMRunning {
			t.Error("expected FPMRunning = false with no PHP version")
		}
	})

	t.Run("FPM running", func(t *testing.T) {
		origContainer := containerRunningFn
		containerRunningFn = func(name string) (bool, error) {
			if name == "lerd-php84-fpm" {
				return true, nil
			}
			return false, nil
		}
		defer func() { containerRunningFn = origContainer }()

		e := &EnrichedSite{PHPVersion: "8.4"}
		e.enrichFPM()
		if !e.FPMRunning {
			t.Error("expected FPMRunning = true")
		}
	})
}
