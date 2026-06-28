package config

import (
	"os"
	"testing"
)

// setConfigDir points ConfigDir() and DataDir() at a temp directory.
func setConfigDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
}

// ── LoadGlobal ────────────────────────────────────────────────────────────────

func TestLoadGlobal_Defaults(t *testing.T) {
	setConfigDir(t)
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if cfg.PHP.DefaultVersion == "" {
		t.Error("expected a default PHP version")
	}
	if cfg.DNS.TLD == "" {
		t.Error("expected a default DNS TLD")
	}
	if !cfg.DNS.Enabled {
		t.Error("expected DNS.Enabled to default true")
	}
	if cfg.Nginx.HTTPPort == 0 {
		t.Error("expected a non-zero HTTP port")
	}
	if cfg.Nginx.HTTPSPort == 0 {
		t.Error("expected a non-zero HTTPS port")
	}
}

// A config returned by LoadGlobal must not share its PHP.Packages map with the
// cache: php:pkg's AddPackage/RemovePackage mutate the loaded copy in place
// before SaveGlobal, and that must not corrupt what later LoadGlobal calls see.
func TestLoadGlobal_PackagesNotAliasedWithCache(t *testing.T) {
	setConfigDir(t)

	seed, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	seed.AddPackage("8.4", "vim")
	if err := SaveGlobal(seed); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	loaded, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	// Mutate the loaded copy without saving — must not reach the cache.
	loaded.AddPackage("8.4", "htop")

	again, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	got := again.GetPackages("8.4")
	if len(got) != 1 || got[0] != "vim" {
		t.Errorf("cache corrupted by an unsaved mutation: GetPackages = %v, want [vim]", got)
	}
}

func TestSaveLoadGlobal_RoundTrip(t *testing.T) {
	setConfigDir(t)

	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}

	cfg.PHP.DefaultVersion = "8.2"
	cfg.Node.DefaultVersion = "20"
	cfg.DNS.TLD = "local"
	cfg.Nginx.HTTPPort = 8080

	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	got, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal after save: %v", err)
	}
	if got.PHP.DefaultVersion != "8.2" {
		t.Errorf("PHP.DefaultVersion = %q, want %q", got.PHP.DefaultVersion, "8.2")
	}
	if got.Node.DefaultVersion != "20" {
		t.Errorf("Node.DefaultVersion = %q, want %q", got.Node.DefaultVersion, "20")
	}
	if got.DNS.TLD != "local" {
		t.Errorf("DNS.TLD = %q, want %q", got.DNS.TLD, "local")
	}
	if got.Nginx.HTTPPort != 8080 {
		t.Errorf("Nginx.HTTPPort = %d, want 8080", got.Nginx.HTTPPort)
	}
}

// ── RequestTimeoutSeconds ─────────────────────────────────────────────────────

func TestRequestTimeoutSeconds_DefaultsTo60WhenUnset(t *testing.T) {
	cfg := &GlobalConfig{}
	if got := cfg.RequestTimeoutSeconds(); got != DefaultRequestTimeout {
		t.Errorf("RequestTimeoutSeconds = %d, want %d", got, DefaultRequestTimeout)
	}
}

func TestRequestTimeoutSeconds_HonoursConfiguredValue(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.Nginx.RequestTimeout = 300
	if got := cfg.RequestTimeoutSeconds(); got != 300 {
		t.Errorf("RequestTimeoutSeconds = %d, want 300", got)
	}
}

func TestRequestTimeoutSeconds_NonPositiveFallsBack(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.Nginx.RequestTimeout = -5
	if got := cfg.RequestTimeoutSeconds(); got != DefaultRequestTimeout {
		t.Errorf("RequestTimeoutSeconds = %d, want %d for non-positive", got, DefaultRequestTimeout)
	}
}

func TestSaveLoadGlobal_RequestTimeoutRoundTrip(t *testing.T) {
	setConfigDir(t)
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.Nginx.RequestTimeout = 240
	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	got, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal after save: %v", err)
	}
	if got.Nginx.RequestTimeout != 240 {
		t.Errorf("Nginx.RequestTimeout = %d, want 240", got.Nginx.RequestTimeout)
	}
}

// ── Cache ─────────────────────────────────────────────────────────────────────

func TestLoadGlobal_CacheReturnsIndependentCopy(t *testing.T) {
	setConfigDir(t)
	invalidateGlobalCache()
	t.Cleanup(invalidateGlobalCache)

	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.DNS.TLD = "local"
	if cfg.Services == nil {
		cfg.Services = map[string]ServiceConfig{}
	}
	cfg.Services["mutated"] = ServiceConfig{Enabled: true}

	again, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal #2: %v", err)
	}
	if again.DNS.TLD == "local" {
		t.Error("cached value should not reflect caller mutation of DNS.TLD")
	}
	if _, ok := again.Services["mutated"]; ok {
		t.Error("cached value should not reflect caller mutation of Services map")
	}
}

func TestLoadGlobal_CacheInvalidatedBySaveGlobal(t *testing.T) {
	setConfigDir(t)
	invalidateGlobalCache()
	t.Cleanup(invalidateGlobalCache)

	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.DNS.TLD = "local"
	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	got, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal after save: %v", err)
	}
	if got.DNS.TLD != "local" {
		t.Errorf("after SaveGlobal, DNS.TLD = %q, want %q", got.DNS.TLD, "local")
	}
}

// ── Xdebug ────────────────────────────────────────────────────────────────────

func TestXdebug_Toggle(t *testing.T) {
	cfg := &GlobalConfig{}

	if cfg.IsXdebugEnabled("8.3") {
		t.Error("expected xdebug disabled by default")
	}

	cfg.SetXdebug("8.3", true)
	if !cfg.IsXdebugEnabled("8.3") {
		t.Error("expected xdebug enabled after SetXdebug(true)")
	}

	cfg.SetXdebug("8.3", false)
	if cfg.IsXdebugEnabled("8.3") {
		t.Error("expected xdebug disabled after SetXdebug(false)")
	}
}

func TestXdebug_IndependentVersions(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.SetXdebug("8.3", true)
	cfg.SetXdebug("8.4", false)

	if !cfg.IsXdebugEnabled("8.3") {
		t.Error("8.3 should still be enabled")
	}
	if cfg.IsXdebugEnabled("8.4") {
		t.Error("8.4 should remain disabled")
	}
}

func TestXdebug_ModeRoundtrip(t *testing.T) {
	cfg := &GlobalConfig{}

	cfg.SetXdebugMode("8.3", "coverage")
	if cfg.GetXdebugMode("8.3") != "coverage" {
		t.Errorf("GetXdebugMode = %q, want %q", cfg.GetXdebugMode("8.3"), "coverage")
	}
	if !cfg.IsXdebugEnabled("8.3") {
		t.Error("IsXdebugEnabled should be true when a mode is set")
	}

	cfg.SetXdebugMode("8.3", "debug,coverage")
	if cfg.GetXdebugMode("8.3") != "debug,coverage" {
		t.Errorf("GetXdebugMode = %q, want combo", cfg.GetXdebugMode("8.3"))
	}

	cfg.SetXdebugMode("8.3", "")
	if cfg.IsXdebugEnabled("8.3") {
		t.Error("empty mode should disable xdebug")
	}
}

func TestXdebug_StartRoundtrip(t *testing.T) {
	cfg := &GlobalConfig{}

	// Default is "yes" when unset.
	if got := cfg.GetXdebugStart("8.4"); got != "yes" {
		t.Errorf("default GetXdebugStart = %q, want yes", got)
	}

	cfg.SetXdebugStart("8.4", "trigger")
	if got := cfg.GetXdebugStart("8.4"); got != "trigger" {
		t.Errorf("GetXdebugStart = %q, want trigger", got)
	}

	// Setting back to the default clears the entry so the config stays lean.
	cfg.SetXdebugStart("8.4", "yes")
	if _, ok := cfg.PHP.XdebugStart["8.4"]; ok {
		t.Error(`setting "yes" should clear the stored entry`)
	}
	if got := cfg.GetXdebugStart("8.4"); got != "yes" {
		t.Errorf("after clear GetXdebugStart = %q, want yes", got)
	}
}

// Legacy configs (lerd <= 1.15.1) only wrote xdebug_enabled. GetXdebugMode
// must fall back to "debug" for those entries so upgrade keeps working.
func TestXdebug_LegacyEnabledFallsBackToDebug(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.PHP.XdebugEnabled = map[string]bool{"8.3": true}

	if got := cfg.GetXdebugMode("8.3"); got != "debug" {
		t.Errorf("legacy xdebug_enabled → GetXdebugMode = %q, want %q", got, "debug")
	}
	if !cfg.IsXdebugEnabled("8.3") {
		t.Error("IsXdebugEnabled should honour legacy flag")
	}
}

func TestXdebug_SetXdebugDefaultsToDebugMode(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.SetXdebug("8.3", true)
	if got := cfg.GetXdebugMode("8.3"); got != "debug" {
		t.Errorf("SetXdebug(true) → mode = %q, want %q", got, "debug")
	}
}

// ── Extensions ────────────────────────────────────────────────────────────────

func TestExtensions_AddRemoveGet(t *testing.T) {
	cfg := &GlobalConfig{}

	if exts := cfg.GetExtensions("8.3"); exts != nil {
		t.Errorf("expected nil extensions, got %v", exts)
	}

	cfg.AddExtension("8.3", "redis")
	cfg.AddExtension("8.3", "imagick")

	exts := cfg.GetExtensions("8.3")
	if len(exts) != 2 {
		t.Fatalf("expected 2 extensions, got %d: %v", len(exts), exts)
	}

	cfg.RemoveExtension("8.3", "redis")
	exts = cfg.GetExtensions("8.3")
	if len(exts) != 1 || exts[0] != "imagick" {
		t.Errorf("expected [imagick] after remove, got %v", exts)
	}
}

func TestExtApkDeps_SetGetClear(t *testing.T) {
	cfg := &GlobalConfig{}

	if deps := cfg.GetExtApkDeps("imap"); deps != nil {
		t.Errorf("expected nil deps, got %v", deps)
	}

	cfg.SetExtApkDeps("imap", []string{"imap-dev", "krb5-dev"})
	if got := cfg.GetExtApkDeps("imap"); len(got) != 2 || got[0] != "imap-dev" || got[1] != "krb5-dev" {
		t.Fatalf("expected [imap-dev krb5-dev], got %v", got)
	}
	if all := cfg.AllExtApkDeps(); len(all) != 1 {
		t.Errorf("expected 1 entry in AllExtApkDeps, got %v", all)
	}

	// SetExtApkDeps with empty deps clears the entry and nils the map.
	cfg.SetExtApkDeps("imap", nil)
	if cfg.GetExtApkDeps("imap") != nil || cfg.AllExtApkDeps() != nil {
		t.Errorf("empty SetExtApkDeps should clear the entry and the map")
	}
}

func TestExtApkDeps_DroppedWhenExtensionRemoved(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.AddExtension("8.4", "ssh2")
	cfg.AddExtension("8.5", "ssh2")
	cfg.SetExtApkDeps("ssh2", []string{"libssh2-dev"})

	// Still used by 8.5, so deps stay.
	cfg.RemoveExtension("8.4", "ssh2")
	if cfg.GetExtApkDeps("ssh2") == nil {
		t.Error("deps should remain while another version still uses the extension")
	}

	// No version uses it anymore, so deps go too.
	cfg.RemoveExtension("8.5", "ssh2")
	if cfg.GetExtApkDeps("ssh2") != nil {
		t.Error("deps should be dropped once no version uses the extension")
	}
}

func TestExtApkDeps_DeepCopied(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.SetExtApkDeps("imap", []string{"imap-dev"})
	clone := cloneGlobalConfig(cfg)
	clone.SetExtApkDeps("imap", []string{"imap-dev", "krb5-dev"})
	if len(cfg.GetExtApkDeps("imap")) != 1 {
		t.Errorf("mutating the clone must not affect the original: %v", cfg.GetExtApkDeps("imap"))
	}
}

func TestExtensions_AddIdempotent(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.AddExtension("8.3", "redis")
	cfg.AddExtension("8.3", "redis")

	if len(cfg.GetExtensions("8.3")) != 1 {
		t.Error("duplicate add should be a no-op")
	}
}

func TestExtensions_RemoveLastCleansMap(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.AddExtension("8.3", "redis")
	cfg.RemoveExtension("8.3", "redis")

	if exts := cfg.GetExtensions("8.3"); len(exts) != 0 {
		t.Errorf("expected empty after removing last ext, got %v", exts)
	}
}

func TestExtensions_RemoveNonExistent(t *testing.T) {
	cfg := &GlobalConfig{}
	// Should not panic
	cfg.RemoveExtension("8.3", "nonexistent")
}

func TestExtensions_IndependentVersions(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.AddExtension("8.3", "redis")
	cfg.AddExtension("8.4", "imagick")

	if exts := cfg.GetExtensions("8.3"); len(exts) != 1 || exts[0] != "redis" {
		t.Errorf("8.3 extensions wrong: %v", exts)
	}
	if exts := cfg.GetExtensions("8.4"); len(exts) != 1 || exts[0] != "imagick" {
		t.Errorf("8.4 extensions wrong: %v", exts)
	}
}

// Pre-existing configs from before the dns.enabled field was introduced have
// no `enabled:` key under `dns:`. LoadGlobal must preserve the `true` default
// for those users so an upgrade does not silently disable DNS.
func TestDNSEnabled_DefaultsTrueWhenKeyAbsent(t *testing.T) {
	setConfigDir(t)
	invalidateGlobalCache()
	t.Cleanup(invalidateGlobalCache)

	if err := os.MkdirAll(ConfigDir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := []byte("dns:\n  tld: test\nphp:\n  default_version: 8.4\n")
	if err := os.WriteFile(GlobalConfigFile(), legacy, 0644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	got, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if !got.DNS.Enabled {
		t.Errorf("DNS.Enabled = false on legacy config without enabled key, want true")
	}
	if got.DNS.TLD != "test" {
		t.Errorf("DNS.TLD = %q, want %q", got.DNS.TLD, "test")
	}
}

func TestDNSEnabled_RoundTripsThroughYAML(t *testing.T) {
	setConfigDir(t)
	invalidateGlobalCache()
	t.Cleanup(invalidateGlobalCache)
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.DNS.Enabled = false
	cfg.DNS.TLD = "localhost"
	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	got, err := LoadGlobal()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.DNS.Enabled {
		t.Errorf("DNS.Enabled = true, want false after roundtrip")
	}
	if got.DNS.TLD != "localhost" {
		t.Errorf("DNS.TLD = %q, want %q", got.DNS.TLD, "localhost")
	}
}

func TestMigrateStaleServiceImages_LeavesTrackLatestAlone(t *testing.T) {
	// Once postgres opted into track_latest, defaultConfig leaves its Image
	// empty so EnsureDefaultPresetQuadlet can resolve the actual newest tag
	// at install time. The stale-image migration must NOT rewrite to that
	// empty seed — doing so would land users in the fresh-install branch and
	// silently bump their data dir across major lines.
	cfg := defaultConfig()
	cfg.Services["postgres"] = ServiceConfig{
		Enabled: false,
		Image:   "postgres:16-alpine",
		Port:    5432,
	}
	migrateStaleServiceImages(cfg)
	if got := cfg.Services["postgres"].Image; got != "postgres:16-alpine" {
		t.Errorf("track_latest preset must keep saved image untouched, got %q", got)
	}
}

func TestMigrateStaleServiceImages_KeepsCustom(t *testing.T) {
	cfg := defaultConfig()
	cfg.Services["postgres"] = ServiceConfig{
		Enabled: true,
		Image:   "myorg/custom-postgres:latest",
		Port:    5432,
	}
	migrateStaleServiceImages(cfg)
	if got := cfg.Services["postgres"].Image; got != "myorg/custom-postgres:latest" {
		t.Errorf("custom postgres image was overwritten: got %q", got)
	}
}

// ── Workers.ExecMode ──────────────────────────────────────────────────────────

func TestWorkerExecMode_Defaults(t *testing.T) {
	cfg := defaultConfig()
	if got := cfg.WorkerExecMode(); got != WorkerExecModeExec {
		t.Errorf("default WorkerExecMode: got %q, want %q", got, WorkerExecModeExec)
	}
}

func TestWorkerExecMode_RespectsContainer(t *testing.T) {
	cfg := defaultConfig()
	cfg.Workers.ExecMode = WorkerExecModeContainer
	if got := cfg.WorkerExecMode(); got != WorkerExecModeContainer {
		t.Errorf("container override not respected: got %q", got)
	}
}

func TestWorkerExecMode_NormalizesUnknownValue(t *testing.T) {
	cfg := defaultConfig()
	cfg.Workers.ExecMode = "garbage"
	if got := cfg.WorkerExecMode(); got != WorkerExecModeExec {
		t.Errorf("unknown value should normalize to exec, got %q", got)
	}
}

func TestWorkerExecMode_RoundTripsThroughYAML(t *testing.T) {
	setConfigDir(t)
	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.Workers.ExecMode = WorkerExecModeContainer
	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	reloaded, err := LoadGlobal()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.WorkerExecMode(); got != WorkerExecModeContainer {
		t.Errorf("after round trip: got %q, want %q", got, WorkerExecModeContainer)
	}
}

// ── Notifications ─────────────────────────────────────────────────────────────

func TestNotifications_DefaultEnabled(t *testing.T) {
	cfg := &GlobalConfig{}
	if !cfg.IsNotificationsEnabled() {
		t.Error("zero-value config should report notifications enabled")
	}
}

func TestNotifications_Toggle(t *testing.T) {
	cfg := &GlobalConfig{}
	cfg.SetNotificationsEnabled(false)
	if cfg.IsNotificationsEnabled() {
		t.Error("after SetNotificationsEnabled(false), IsNotificationsEnabled should be false")
	}
	if !cfg.Notifications.Disabled {
		t.Error("Notifications.Disabled should be true when disabled")
	}
	cfg.SetNotificationsEnabled(true)
	if !cfg.IsNotificationsEnabled() {
		t.Error("after SetNotificationsEnabled(true), IsNotificationsEnabled should be true")
	}
	if cfg.Notifications.Disabled {
		t.Error("Notifications.Disabled should be false when enabled")
	}
}

func TestNotifications_DefaultsEnabledForLegacyConfig(t *testing.T) {
	setConfigDir(t)
	invalidateGlobalCache()
	t.Cleanup(invalidateGlobalCache)

	if err := os.MkdirAll(ConfigDir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := []byte("php:\n  default_version: 8.4\n")
	if err := os.WriteFile(GlobalConfigFile(), legacy, 0644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	got, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if !got.IsNotificationsEnabled() {
		t.Error("legacy config without notifications key should default to enabled")
	}
}

func TestNotifications_RoundTripsThroughYAML(t *testing.T) {
	setConfigDir(t)
	invalidateGlobalCache()
	t.Cleanup(invalidateGlobalCache)

	cfg, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.SetNotificationsEnabled(false)
	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	got, err := LoadGlobal()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.IsNotificationsEnabled() {
		t.Error("notifications should remain disabled after YAML round trip")
	}
}

func TestDNSManaged(t *testing.T) {
	var nilCfg *GlobalConfig
	if !nilCfg.DNSManaged() {
		t.Error("nil config should count as DNS-managed, matching the rest of the codebase")
	}
	enabled := &GlobalConfig{}
	enabled.DNS.Enabled = true
	if !enabled.DNSManaged() {
		t.Error("DNSManaged() = false with DNS.Enabled true, want true")
	}
	disabled := &GlobalConfig{}
	disabled.DNS.Enabled = false
	if disabled.DNSManaged() {
		t.Error("DNSManaged() = true with DNS.Enabled false, want false")
	}
}

// TestServiceConfig_HostPorts is the single source both the serviceops port guard
// and the host-proxy allocator consume, so it must capture Port, the PublishedPort
// override, and every ExtraPorts mapping form — including "ip:host:container",
// whose host side the old guard parser dropped.
func TestServiceConfig_HostPorts(t *testing.T) {
	svc := ServiceConfig{
		Port:          3306,
		PublishedPort: 3307,
		ExtraPorts:    []string{"8082:8081", "127.0.0.1:9090:9090", "7000", "6379/tcp"},
	}
	got := map[int]bool{}
	for _, p := range svc.HostPorts() {
		got[p] = true
	}
	for _, want := range []int{3306, 3307, 8082, 9090, 7000, 6379} {
		if !got[want] {
			t.Errorf("HostPorts missing %d; got %v", want, got)
		}
	}
	// The container-side ports must never be reserved as host ports.
	if got[8081] {
		t.Errorf("HostPorts wrongly reserved container-side port 8081: %v", got)
	}
}
