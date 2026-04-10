package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
