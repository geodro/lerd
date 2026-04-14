package siteinfo

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/applog"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	gitpkg "github.com/geodro/lerd/internal/git"
	nodePkg "github.com/geodro/lerd/internal/node"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
)

// EnrichFlag controls which enrichment steps run during site loading.
type EnrichFlag uint32

const (
	EnrichFramework       EnrichFlag = 1 << iota // framework label + version
	EnrichVersions                               // live PHP/Node detection from disk
	EnrichWorkers                                // worker status via podman
	EnrichFPM                                    // FPM container running check
	EnrichGit                                    // worktrees + main branch
	EnrichServices                               // .env + .lerd.yaml service detection
	EnrichDomainConflicts                        // conflicting domain check
	EnrichLogs                                   // app log file detection
	EnrichFavicon                                // favicon detection
	EnrichStripe                                 // stripe secret check

	EnrichCLI = EnrichFramework | EnrichGit
	EnrichMCP = EnrichFramework | EnrichWorkers
	EnrichUI  = EnrichFramework | EnrichVersions | EnrichWorkers |
		EnrichFPM | EnrichGit | EnrichServices |
		EnrichDomainConflicts | EnrichLogs | EnrichFavicon | EnrichStripe
)

// WorkerInfo describes a framework worker and its runtime state.
type WorkerInfo struct {
	Name    string
	Label   string
	Running bool
	Failing bool
}

// WorktreeInfo describes a git worktree associated with a site.
type WorktreeInfo struct {
	Branch string
	Domain string
	Path   string
}

// ConflictingDomain describes a domain declared in .lerd.yaml that is owned
// by a different site on this machine.
type ConflictingDomain struct {
	Domain  string
	OwnedBy string
}

// EnrichedSite is the superset of site information needed by all surfaces.
type EnrichedSite struct {
	// Base fields from config.Site
	Name          string
	Domains       []string
	Path          string
	PHPVersion    string
	NodeVersion   string
	Secured       bool
	Paused        bool
	PausedWorkers []string
	PublicDir     string
	AppURL        string

	// Framework
	FrameworkName    string
	FrameworkLabel   string
	FrameworkVersion string

	// Runtime status
	FPMRunning bool

	// Well-known workers
	HasQueueWorker    bool
	QueueRunning      bool
	QueueFailing      bool
	HasScheduleWorker bool
	ScheduleRunning   bool
	ScheduleFailing   bool
	HasReverb         bool
	ReverbRunning     bool
	ReverbFailing     bool
	HasHorizon        bool
	HorizonRunning    bool
	HorizonFailing    bool
	StripeSecretSet   bool
	StripeRunning     bool

	// Custom framework workers
	FrameworkWorkers []WorkerInfo

	// Git
	Branch    string
	Worktrees []WorktreeInfo

	// Domain conflicts
	ConflictingDomains []ConflictingDomain

	// Services
	Services []string

	// LAN sharing
	LANPort int

	// App metadata
	HasAppLogs    bool
	LatestLogTime string
	HasFavicon    bool

	// Version change tracking (for write-back by caller)
	PHPVersionChanged   bool
	NodeVersionChanged  bool
	OriginalPHPVersion  string
	OriginalNodeVersion string
}

// PrimaryDomain returns the first domain or empty string.
func (e *EnrichedSite) PrimaryDomain() string {
	if len(e.Domains) > 0 {
		return e.Domains[0]
	}
	return ""
}

// KnownServices lists the built-in service names used for auto-detection.
var KnownServices = []string{"mysql", "redis", "postgres", "meilisearch", "rustfs", "mailpit"}

// faviconCandidates lists file names to probe when looking for a site's favicon.
var faviconCandidates = []string{
	"favicon.ico",
	"favicon.svg",
	"favicon.png",
}

// Swappable function variables for testing without podman/systemd.
// unitStatusFn routes through a batched systemctl cache so loading 25 sites
// no longer fans out into 125+ subprocesses per /api/sites request.
var (
	unitStatusFn       = unitStatusCached
	containerRunningFn = func(name string) (bool, error) {
		return podman.Cache.Running(name), nil
	}
)

// LoadAll loads all non-ignored sites and enriches them according to flags.
func LoadAll(flags EnrichFlag) ([]EnrichedSite, error) {
	reg, err := config.LoadSites()
	if err != nil {
		return nil, err
	}

	// Filter ignored sites first so we know the final length up front and
	// can write straight into the result slice from goroutines.
	visible := reg.Sites[:0:0]
	for _, s := range reg.Sites {
		if s.Ignored {
			continue
		}
		visible = append(visible, s)
	}

	result := make([]EnrichedSite, len(visible))
	if len(visible) == 0 {
		return []EnrichedSite{}, nil
	}

	// Per-site enrichment fans out to many independent subprocesses
	// (systemctl is-active, podman ps, git status, …). Sequential calls
	// dominated /api/sites latency at ~750ms for 25 sites; parallelising
	// across sites cuts that proportionally and prevents the UI's 5s
	// polling loop from piling up against the browser's per-host
	// HTTP/1.1 connection limit.
	var wg sync.WaitGroup
	wg.Add(len(visible))
	for i := range visible {
		i := i
		go func() {
			defer wg.Done()
			result[i] = Enrich(visible[i], flags)
		}()
	}
	wg.Wait()
	return result, nil
}

// Enrich populates an EnrichedSite from a config.Site according to the given flags.
func Enrich(s config.Site, flags EnrichFlag) EnrichedSite {
	e := EnrichedSite{
		Name:                s.Name,
		Domains:             s.Domains,
		Path:                s.Path,
		PHPVersion:          s.PHPVersion,
		NodeVersion:         s.NodeVersion,
		Secured:             s.Secured,
		Paused:              s.Paused,
		PausedWorkers:       s.PausedWorkers,
		PublicDir:           s.PublicDir,
		AppURL:              s.AppURL,
		LANPort:             s.LANPort,
		FrameworkName:       s.Framework,
		OriginalPHPVersion:  s.PHPVersion,
		OriginalNodeVersion: s.NodeVersion,
	}

	var fw *config.Framework
	var hasFw bool

	if flags&EnrichFramework != 0 || flags&EnrichWorkers != 0 || flags&EnrichLogs != 0 || flags&EnrichFavicon != 0 {
		fw, hasFw = config.GetFrameworkForDir(s.Framework, s.Path)
		if hasFw {
			e.FrameworkVersion = fw.Version
		}
	}

	if flags&EnrichFramework != 0 {
		e.FrameworkLabel = frameworkLabel(s.Framework, s.Path, fw, hasFw)
	}

	if flags&EnrichVersions != 0 {
		e.enrichVersions(s, fw, hasFw)
	}

	if flags&EnrichFPM != 0 {
		e.enrichFPM()
	}

	if flags&EnrichStripe != 0 {
		e.enrichStripe()
	}

	if flags&EnrichWorkers != 0 {
		e.enrichWorkers(fw, hasFw)
	}

	if flags&EnrichGit != 0 {
		e.enrichGit()
	}

	if flags&EnrichServices != 0 {
		e.enrichServices()
	}

	if flags&EnrichDomainConflicts != 0 {
		e.enrichDomainConflicts()
	}

	if flags&EnrichLogs != 0 {
		e.enrichLogs(fw, hasFw)
	}

	if flags&EnrichFavicon != 0 {
		e.HasFavicon = DetectFavicon(s.Path, s.PublicDir, s.Framework, fw, hasFw) != ""
	}

	return e
}

// PersistVersionChanges writes back any detected version changes to the site registry.
func PersistVersionChanges(sites []EnrichedSite) error {
	for _, e := range sites {
		if !e.PHPVersionChanged && !e.NodeVersionChanged {
			continue
		}
		s, err := config.FindSite(e.Name)
		if err != nil {
			continue
		}
		if e.PHPVersionChanged {
			s.PHPVersion = e.PHPVersion
		}
		if e.NodeVersionChanged {
			s.NodeVersion = e.NodeVersion
		}
		if err := config.AddSite(*s); err != nil {
			return err
		}
	}
	return nil
}

func (e *EnrichedSite) enrichVersions(s config.Site, fw *config.Framework, hasFw bool) {
	phpMin, phpMax := "", ""
	if hasFw {
		phpMin, phpMax = fw.PHP.Min, fw.PHP.Max
	}
	detected := phpPkg.DetectVersionClamped(s.Path, phpMin, phpMax, s.PHPVersion)
	if detected != s.PHPVersion {
		e.PHPVersion = detected
		e.PHPVersionChanged = true
	}

	if nodeDetected, err := nodePkg.DetectVersion(s.Path); err == nil && nodeDetected != "" {
		if nodeDetected != s.NodeVersion {
			e.NodeVersion = nodeDetected
			e.NodeVersionChanged = true
		}
	}
	if strings.Trim(e.NodeVersion, "0123456789") != "" {
		e.NodeVersion = ""
	}
}

func (e *EnrichedSite) enrichFPM() {
	if e.PHPVersion != "" {
		short := strings.ReplaceAll(e.PHPVersion, ".", "")
		e.FPMRunning, _ = containerRunningFn("lerd-php" + short + "-fpm")
	}
}

func (e *EnrichedSite) enrichStripe() {
	if envfile.ReadKey(filepath.Join(e.Path, ".env"), "STRIPE_SECRET") != "" {
		e.StripeSecretSet = true
		status, _ := unitStatusFn("lerd-stripe-" + e.Name)
		e.StripeRunning = status == "active"
	}
}

func (e *EnrichedSite) enrichWorkers(fw *config.Framework, hasFw bool) {
	if !hasFw || fw.Workers == nil {
		return
	}

	// Build suppressed set from running workers with ConflictsWith.
	suppressed := make(map[string]bool)
	for wn, wDef := range fw.Workers {
		if len(wDef.ConflictsWith) == 0 {
			continue
		}
		if st, _ := unitStatusFn("lerd-" + wn + "-" + e.Name); st == "active" {
			for _, c := range wDef.ConflictsWith {
				suppressed[c] = true
			}
		}
	}

	// Well-known workers
	if fw.HasWorker("queue", e.Path) && !suppressed["queue"] {
		e.HasQueueWorker = true
		status, _ := unitStatusFn("lerd-queue-" + e.Name)
		e.QueueRunning = status == "active" || status == "activating"
		e.QueueFailing = status == "failed"
	}
	if fw.HasWorker("schedule", e.Path) && !suppressed["schedule"] {
		e.HasScheduleWorker = true
		status, _ := unitStatusFn("lerd-schedule-" + e.Name)
		// Timer-driven scheduler: .service is static between firings.
		if status != "active" && status != "activating" {
			if t, _ := unitStatusFn("lerd-schedule-" + e.Name + ".timer"); t == "active" {
				status = "active"
			}
		}
		e.ScheduleRunning = status == "active" || status == "activating"
		e.ScheduleFailing = status == "failed"
	}
	if fw.HasWorker("reverb", e.Path) && !suppressed["reverb"] {
		e.HasReverb = true
		status, _ := unitStatusFn("lerd-reverb-" + e.Name)
		e.ReverbRunning = status == "active" || status == "activating"
		e.ReverbFailing = status == "failed"
	}
	if fw.HasWorker("horizon", e.Path) && !suppressed["horizon"] {
		e.HasHorizon = true
		status, _ := unitStatusFn("lerd-horizon-" + e.Name)
		e.HorizonRunning = status == "active" || status == "activating"
		e.HorizonFailing = status == "failed"
		e.HasQueueWorker = false // Horizon manages queues
	}

	// Custom framework workers
	names := make([]string, 0, len(fw.Workers))
	for n, wDef := range fw.Workers {
		switch n {
		case "queue", "schedule", "reverb", "horizon":
			continue
		}
		if wDef.Check != nil && !config.MatchesRule(e.Path, *wDef.Check) {
			continue
		}
		if suppressed[n] {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	for _, wname := range names {
		w := fw.Workers[wname]
		unitStatus, _ := unitStatusFn("lerd-" + wname + "-" + e.Name)
		label := w.Label
		if label == "" {
			label = wname
		}
		e.FrameworkWorkers = append(e.FrameworkWorkers, WorkerInfo{
			Name:    wname,
			Label:   label,
			Running: unitStatus == "active" || unitStatus == "activating",
			Failing: unitStatus == "failed",
		})
	}
}

func (e *EnrichedSite) enrichGit() {
	e.Branch = gitpkg.MainBranch(e.Path)
	if wts, err := gitpkg.DetectWorktrees(e.Path, e.PrimaryDomain()); err == nil {
		for _, wt := range wts {
			e.Worktrees = append(e.Worktrees, WorktreeInfo{
				Branch: wt.Branch,
				Domain: wt.Domain,
				Path:   wt.Path,
			})
		}
	}
}

func (e *EnrichedSite) enrichServices() {
	proj, projErr := config.LoadProjectConfig(e.Path)
	svcSet := make(map[string]bool)

	if projErr == nil && proj != nil {
		for _, ps := range proj.Services {
			if ps.Name != "" && !svcSet[ps.Name] {
				e.Services = append(e.Services, ps.Name)
				svcSet[ps.Name] = true
			}
		}
	}

	envData, err := os.ReadFile(filepath.Join(e.Path, ".env"))
	if err != nil {
		return
	}
	envStr := string(envData)
	for _, svcName := range KnownServices {
		if !svcSet[svcName] && strings.Contains(envStr, "lerd-"+svcName) {
			e.Services = append(e.Services, svcName)
			svcSet[svcName] = true
		}
	}
	if customs, err := config.ListCustomServices(); err == nil {
		for _, cs := range customs {
			if !svcSet[cs.Name] && strings.Contains(envStr, "lerd-"+cs.Name) {
				e.Services = append(e.Services, cs.Name)
				svcSet[cs.Name] = true
			}
		}
	}
}

func (e *EnrichedSite) enrichDomainConflicts() {
	proj, err := config.LoadProjectConfig(e.Path)
	if err != nil || proj == nil || len(proj.Domains) == 0 {
		return
	}

	gcfg, _ := config.LoadGlobal()
	tld := ""
	if gcfg != nil {
		tld = gcfg.DNS.TLD
	}

	registered := make(map[string]bool, len(e.Domains))
	for _, d := range e.Domains {
		registered[d] = true
	}

	for _, declared := range proj.Domains {
		full := strings.ToLower(declared)
		if tld != "" {
			full = full + "." + tld
		}
		if registered[full] {
			continue
		}
		owner := ""
		if owning, _ := config.IsDomainUsed(full); owning != nil && owning.Path != e.Path {
			owner = owning.Name
		}
		e.ConflictingDomains = append(e.ConflictingDomains, ConflictingDomain{
			Domain:  full,
			OwnedBy: owner,
		})
	}
}

func (e *EnrichedSite) enrichLogs(fw *config.Framework, hasFw bool) {
	e.HasAppLogs = hasLogFiles(hasFw, fw, e.Path)
	e.LatestLogTime = latestLogTime(hasFw, fw, e.Path)
}

func frameworkLabel(name, path string, fw *config.Framework, hasFw bool) string {
	if name == "" {
		return ""
	}
	if hasFw {
		if fw.Version != "" {
			return fw.Label + " " + fw.Version
		}
		return fw.Label
	}
	return name
}

// FrameworkLabel returns the display label for a framework name.
// Exported for use by callers that need the label without full enrichment.
func FrameworkLabel(name, path string) string {
	if name == "" {
		return ""
	}
	fw, hasFw := config.GetFrameworkForDir(name, path)
	return frameworkLabel(name, path, fw, hasFw)
}

func hasLogFiles(hasFw bool, fw *config.Framework, projectPath string) bool {
	if !hasFw || len(fw.Logs) == 0 {
		return false
	}
	for _, src := range fw.Logs {
		matches, _ := filepath.Glob(filepath.Join(projectPath, src.Path))
		if len(matches) > 0 {
			return true
		}
	}
	return false
}

func latestLogTime(hasFw bool, fw *config.Framework, projectPath string) string {
	if !hasFw || len(fw.Logs) == 0 {
		return ""
	}
	t := applog.LatestModTime(projectPath, fw.Logs)
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// DetectFavicon returns the absolute path of the first favicon file found in
// the site's public directory, or empty string if none found.
// When fw/hasFw are not available, pass nil/false and the function will look
// them up from the framework name.
func DetectFavicon(sitePath, publicDir, framework string, fw *config.Framework, hasFw bool) string {
	if fw == nil && framework != "" {
		fw, hasFw = config.GetFrameworkForDir(framework, sitePath)
	}
	if publicDir == "" {
		if hasFw && fw.PublicDir != "" {
			publicDir = fw.PublicDir
		} else {
			publicDir = config.DetectPublicDir(sitePath)
		}
	}
	base := sitePath
	if publicDir != "." {
		base = filepath.Join(sitePath, publicDir)
	}
	if hasFw && fw.Favicon != "" {
		p := filepath.Join(base, fw.Favicon)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Size() > 0 {
			return p
		}
	}
	for _, name := range faviconCandidates {
		p := filepath.Join(base, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Size() > 0 {
			return p
		}
	}
	return ""
}
