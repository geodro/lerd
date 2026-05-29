package mcp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestStripANSI_removesColorCodes(t *testing.T) {
	input := "\x1b[32mOK\x1b[0m some text \x1b[31mFAIL\x1b[0m"
	got := stripANSI(input)
	want := "OK some text FAIL"
	if got != want {
		t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
	}
}

func TestStripANSI_preservesPlainText(t *testing.T) {
	input := "no ansi here"
	got := stripANSI(input)
	if got != input {
		t.Errorf("stripANSI(%q) = %q, want %q", input, got, input)
	}
}

func TestStripANSI_handlesBoldAndCursor(t *testing.T) {
	input := "\x1b[1mBold\x1b[0m \x1b[2J\x1b[H"
	got := stripANSI(input)
	want := "Bold "
	if got != want {
		t.Errorf("stripANSI(%q) = %q, want %q", input, got, want)
	}
}

func TestValidatePHPVersionMCP_valid(t *testing.T) {
	for _, v := range []string{"8.3", "8.4", "7.4"} {
		if err := validatePHPVersionMCP(v); err != nil {
			t.Errorf("validatePHPVersionMCP(%q) = %v, want nil", v, err)
		}
	}
}

func TestValidatePHPVersionMCP_invalid(t *testing.T) {
	for _, v := range []string{"8", "8.3.1", "abc", "", "8.", ".3"} {
		if err := validatePHPVersionMCP(v); err == nil {
			t.Errorf("validatePHPVersionMCP(%q) = nil, want error", v)
		}
	}
}

func TestSiteHasComposerPkg_found(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"laravel/horizon":"^5.0"}}`), 0644)
	if !siteHasComposerPkg(dir, `"laravel/horizon"`) {
		t.Error("expected true for present package")
	}
}

func TestSiteHasComposerPkg_notFound(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"laravel/framework":"^11.0"}}`), 0644)
	if siteHasComposerPkg(dir, `"laravel/horizon"`) {
		t.Error("expected false for missing package")
	}
}

func TestSiteHasComposerPkg_noFile(t *testing.T) {
	if siteHasComposerPkg(t.TempDir(), `"laravel/horizon"`) {
		t.Error("expected false when no composer.json")
	}
}

func TestToolOK_structure(t *testing.T) {
	result := toolOK("hello")
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatal("expected content array with one element")
	}
	if content[0]["type"] != "text" || content[0]["text"] != "hello" {
		t.Errorf("unexpected content: %v", content[0])
	}
	if _, has := result["isError"]; has {
		t.Error("toolOK should not have isError")
	}
}

func TestToolErr_structure(t *testing.T) {
	result := toolErr("oops")
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) != 1 {
		t.Fatal("expected content array with one element")
	}
	if content[0]["text"] != "oops" {
		t.Errorf("unexpected text: %v", content[0]["text"])
	}
	if result["isError"] != true {
		t.Error("toolErr should have isError=true")
	}
}

func TestExecEnvCheck_inSync(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("APP_KEY=\nDB_HOST=\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_KEY=secret\nDB_HOST=localhost\n"), 0644)

	result, rpcErr := execEnvCheck(map[string]any{"path": dir})
	if rpcErr != nil {
		t.Fatal("unexpected rpc error")
	}
	content := result.(map[string]any)["content"].([]map[string]any)
	var parsed struct {
		InSync bool `json:"in_sync"`
		Count  int  `json:"out_of_sync_count"`
	}
	if err := json.Unmarshal([]byte(content[0]["text"].(string)), &parsed); err != nil {
		t.Fatal("failed to parse JSON:", err)
	}
	if !parsed.InSync {
		t.Error("expected in_sync=true")
	}
	if parsed.Count != 0 {
		t.Errorf("expected count=0, got %d", parsed.Count)
	}
}

func TestExecEnvCheck_missingKeys(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env.example"), []byte("APP_KEY=\nDB_HOST=\nMAIL_HOST=\n"), 0644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_KEY=secret\n"), 0644)

	result, rpcErr := execEnvCheck(map[string]any{"path": dir})
	if rpcErr != nil {
		t.Fatal("unexpected rpc error")
	}
	content := result.(map[string]any)["content"].([]map[string]any)
	var parsed struct {
		InSync bool `json:"in_sync"`
		Count  int  `json:"out_of_sync_count"`
		Keys   []struct {
			Key     string          `json:"key"`
			Example bool            `json:"in_example"`
			Files   map[string]bool `json:"files"`
		} `json:"keys"`
	}
	if err := json.Unmarshal([]byte(content[0]["text"].(string)), &parsed); err != nil {
		t.Fatal("failed to parse JSON:", err)
	}
	if parsed.InSync {
		t.Error("expected in_sync=false")
	}
	if parsed.Count != 2 {
		t.Errorf("expected count=2, got %d", parsed.Count)
	}
	for _, k := range parsed.Keys {
		if !k.Example {
			t.Errorf("key %s should be in example", k.Key)
		}
		if k.Files[".env"] {
			t.Errorf("key %s should be missing from .env", k.Key)
		}
	}
}

// TestToolList_underSizeCeiling guards against regrowth of the tools/list
// manifest sent on every MCP session. Every byte above the ceiling is in
// context for the whole session; raise the ceiling only with a justified
// content addition, not by accreting description verbosity.
func TestToolList_underSizeCeiling(t *testing.T) {
	// Bumped to 23000 for the four new dumps_* tools (recent/status/clear/toggle).
	// Descriptions trimmed in dumpToolDefs to keep the delta as small as possible.
	// Bumped to 23200 for the php_ext `apk_deps` parameter (new content, not verbosity).
	// Bumped to 24200 for commands_list / commands_run tools.
	// Bumped to 26000 for command_add / command_remove tools (let agents
	// author .lerd.yaml commands blocks alongside the read/run path).
	// Bumped to 27000 for profiler_toggle / profiler_status (SPX profiler);
	// descriptions trimmed to keep the delta small.
	// Bumped to 29000 for db_snapshot / db_snapshots / db_restore /
	// db_snapshot_delete (database snapshots); descriptions kept terse.
	// Bumped to 29500 for service_config (five actions in one tool:
	// read / write / restore / reset / list_backups); descriptions
	// already trimmed to single-line shape.
	// Bumped to 30200 for site_nginx (read / write / reset the per-site nginx
	// override, with --branch worktree support); description already terse.
	const ceiling = 30200
	got, err := json.Marshal(toolList())
	if err != nil {
		t.Fatalf("marshal tool list: %v", err)
	}
	if len(got) > ceiling {
		t.Errorf("toolList JSON is %d bytes, ceiling is %d — trim before raising", len(got), ceiling)
	}
}

// TestRunComposerInstallIfNeeded_noComposerJsonIsNoop confirms the helper
// silently returns when composer.json doesn't exist (non-PHP scaffolds,
// accidental calls from other framework paths).
func TestRunComposerInstallIfNeeded_noComposerJsonIsNoop(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := runComposerInstallIfNeeded(dir, &buf); err != nil {
		t.Errorf("expected nil for missing composer.json, got %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty buffer, got %q", buf.String())
	}
}

// TestRunComposerInstallIfNeeded_vendorExistsIsNoop confirms the helper
// skips the install when vendor/ is already populated (re-running the tool
// on an existing project should not re-download dependencies).
func TestRunComposerInstallIfNeeded_vendorExistsIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runComposerInstallIfNeeded(dir, &buf); err != nil {
		t.Errorf("expected nil when vendor/ exists, got %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty buffer when vendor/ exists, got %q", buf.String())
	}
}

// TestResolveWorkerCwd_noBranchReturnsSitePath pins the parent-site routing:
// without a branch, lerd worker start/stop runs in site.Path so the CLI's
// workerNames helper picks the parent unit (lerd-<worker>-<site>).
func TestResolveWorkerCwd_noBranchReturnsSitePath(t *testing.T) {
	site := &config.Site{Name: "demo", Path: "/srv/demo"}
	cwd, errResp := resolveWorkerCwd(site, "")
	if errResp != nil {
		t.Fatalf("unexpected error response: %v", errResp)
	}
	if cwd != "/srv/demo" {
		t.Errorf("expected site.Path, got %q", cwd)
	}
}

// TestResolveWorkerCwd_unknownBranchErrors pins the failure path: a branch
// that doesn't resolve to a worktree on disk surfaces a tool-error payload
// instead of silently routing to the parent site (which would start the
// wrong unit).
func TestResolveWorkerCwd_unknownBranchErrors(t *testing.T) {
	site := &config.Site{Name: "demo", Path: t.TempDir()}
	cwd, errResp := resolveWorkerCwd(site, "missing-branch")
	if errResp == nil {
		t.Fatal("expected error response for unknown branch")
	}
	if cwd != "" {
		t.Errorf("expected empty cwd on error, got %q", cwd)
	}
}

// TestExecWorkersMode_RejectsBadAction pins the validation that keeps the
// tool from silently no-op-ing on a typo'd action. Real exec paths shell
// out to the lerd CLI, which we don't run from unit tests; the bad-arg
// branch is what we can pin without an integration setup.
func TestExecWorkersMode_RejectsBadAction(t *testing.T) {
	resp, rpcErr := execWorkersMode(map[string]any{"action": "toggle"})
	if rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}
	got, _ := json.Marshal(resp)
	if !bytes.Contains(got, []byte("get or set")) {
		t.Errorf("bad action should be rejected with hint, got %s", got)
	}
}

// TestExecWorkersMode_SetRequiresValidMode verifies that `set` without a
// valid mode value short-circuits before shelling out so a typo doesn't
// reach the CLI as a real attempt.
func TestExecWorkersMode_SetRequiresValidMode(t *testing.T) {
	resp, rpcErr := execWorkersMode(map[string]any{"action": "set", "mode": "fast"})
	if rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}
	got, _ := json.Marshal(resp)
	if !bytes.Contains(got, []byte("exec or container")) {
		t.Errorf("invalid mode should be rejected, got %s", got)
	}
}

// service_add now accepts an init flag so an MCP-driven agent can wire
// catatonit (--init) for custom services whose main process ignores
// SIGTERM as PID 1 (mysql forks, certain Elasticsearch versions).
func TestExecServiceAdd_InitFlagPersists(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	resp, rpcErr := execServiceAdd(map[string]any{
		"name":  "memcrash",
		"image": "docker.io/library/alpine:3.19",
		"init":  true,
	})
	if rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}
	if got, _ := json.Marshal(resp); bytes.Contains(got, []byte("\"error\"")) {
		t.Fatalf("execServiceAdd reported error: %s", got)
	}

	svc, err := config.LoadCustomService("memcrash")
	if err != nil {
		t.Fatalf("LoadCustomService: %v", err)
	}
	if !svc.Init {
		t.Error("Init flag did not persist to disk")
	}
}

func TestExecServiceAdd_InitDefaultsFalse(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if _, rpcErr := execServiceAdd(map[string]any{
		"name":  "redisish",
		"image": "docker.io/library/alpine:3.19",
	}); rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}

	svc, err := config.LoadCustomService("redisish")
	if err != nil {
		t.Fatalf("LoadCustomService: %v", err)
	}
	if svc.Init {
		t.Error("Init flag should default to false when not provided")
	}
}

// installFakeQuadlet drops a stub .container file on disk so
// serviceops.ServiceInstalled returns true without going through podman.
// Mirrors the helper pattern in serviceops_test and internal/ui tests.
func installFakeQuadlet(t *testing.T, name string) {
	t.Helper()
	dir := config.QuadletDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir quadlet dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lerd-"+name+".container"), []byte("[Container]\n"), 0o644); err != nil {
		t.Fatalf("write fake quadlet: %v", err)
	}
}

// TestExecServiceConfig_ReadSurfacesTemplateOnFirstAccess covers the
// most common MCP-driven path: an agent calls service_config (default
// action=read) to see what tuning knobs the service exposes. The handler
// materialises the seeded template on first read so the response always
// has body content for the model to learn from.
func TestExecServiceConfig_ReadSurfacesTemplateOnFirstAccess(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	installFakeQuadlet(t, "mysql")

	resp, rpcErr := execServiceConfigRead(map[string]any{"name": "mysql"})
	if rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}
	raw, _ := json.Marshal(resp)
	if bytes.Contains(raw, []byte("\"isError\":true")) {
		t.Fatalf("read reported error: %s", raw)
	}
	// The text payload is itself JSON with target/exists/content fields.
	if !bytes.Contains(raw, []byte("/etc/mysql/conf.d/zz-lerd-user.cnf")) {
		t.Errorf("response missing target path: %s", raw)
	}
	if !bytes.Contains(raw, []byte("[mysqld]")) {
		t.Errorf("response missing template body: %s", raw)
	}
}

// TestExecServiceConfig_NotInstalledHintsAtInstall verifies the install
// guard surfaces the same lerd CLI hint the HTTP handler does, so an
// MCP-driven agent can recover without guessing the command.
func TestExecServiceConfig_NotInstalledHintsAtInstall(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	// No installFakeQuadlet: ServiceInstalled returns false.

	resp, _ := execServiceConfigRead(map[string]any{"name": "mysql"})
	raw, _ := json.Marshal(resp)
	if !bytes.Contains(raw, []byte("isError")) {
		t.Fatalf("expected error response: %s", raw)
	}
	if !bytes.Contains(raw, []byte("lerd service preset install mysql")) {
		t.Errorf("expected install hint in error: %s", raw)
	}
}

// TestExecServiceConfig_UnsupportedFamilyListsTunable verifies that a
// service whose family is not on the built-in allowlist (and which has
// no inline TuningSpec) gets a helpful error listing the supported
// families, so the agent can either pick a different service or learn
// to set tuning: inline.
func TestExecServiceConfig_UnsupportedFamilyListsTunable(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := config.SaveCustomService(&config.CustomService{
		Name:   "meilisearch",
		Image:  "docker.io/getmeili/meilisearch:v1",
		Family: "meilisearch",
	}); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}
	installFakeQuadlet(t, "meilisearch")

	resp, _ := execServiceConfigRead(map[string]any{"name": "meilisearch"})
	raw, _ := json.Marshal(resp)
	if !bytes.Contains(raw, []byte("isError")) {
		t.Fatalf("expected error: %s", raw)
	}
	// Hint should mention the inline `tuning:` opt-in so a model can
	// fix the YAML itself.
	if !bytes.Contains(raw, []byte("tuning:")) {
		t.Errorf("expected inline-tuning hint in error: %s", raw)
	}
}

// TestExecServiceConfig_ReadHonoursInlineTuningSpec covers the new
// user-extensible path: a custom service with an inline tuning: block
// surfaces in MCP's read response without lerd having to recognise its
// family.
func TestExecServiceConfig_ReadHonoursInlineTuningSpec(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := config.SaveCustomService(&config.CustomService{
		Name:   "my-cache",
		Image:  "docker.io/library/memcached:1.6-alpine",
		Family: "memcached",
		Tuning: &config.TuningSpec{
			Target:   "/etc/memcached.conf",
			Template: "# user-defined memcached overrides\n",
		},
	}); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}
	installFakeQuadlet(t, "my-cache")

	resp, rpcErr := execServiceConfigRead(map[string]any{"name": "my-cache"})
	if rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}
	raw, _ := json.Marshal(resp)
	if bytes.Contains(raw, []byte("isError")) {
		t.Fatalf("inline tuning should be supported: %s", raw)
	}
	if !bytes.Contains(raw, []byte("/etc/memcached.conf")) {
		t.Errorf("response missing inline target: %s", raw)
	}
	if !bytes.Contains(raw, []byte("user-defined memcached overrides")) {
		t.Errorf("response missing inline template: %s", raw)
	}
}
