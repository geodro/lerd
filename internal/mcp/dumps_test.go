package mcp

import (
	"strings"
	"testing"
)

func TestDumpToolDefs_ListsAllFour(t *testing.T) {
	got := dumpToolDefs()
	names := map[string]bool{}
	for _, d := range got {
		names[d.Name] = true
	}
	for _, want := range []string{"dumps_recent", "dumps_status", "dumps_clear", "dumps_toggle"} {
		if !names[want] {
			t.Errorf("missing tool %q (got %v)", want, names)
		}
	}
}

func TestDumpToolDefs_AppearInToolList(t *testing.T) {
	tools := toolList()
	names := map[string]bool{}
	for _, d := range tools {
		names[d.Name] = true
	}
	for _, want := range []string{"dumps_recent", "dumps_status", "dumps_clear", "dumps_toggle"} {
		if !names[want] {
			t.Errorf("toolList missing %q", want)
		}
	}
}

func TestDumpsToggle_RequiresEnable(t *testing.T) {
	got, rpcErr := execDumpsToggle(map[string]any{})
	if rpcErr != nil {
		t.Fatalf("unexpected rpcErr: %v", rpcErr)
	}
	body := toolText(got)
	if !strings.Contains(body, "required") {
		t.Errorf("expected error about required enable, got %q", body)
	}
}

func TestDumpsToggle_RejectsWrongType(t *testing.T) {
	got, _ := execDumpsToggle(map[string]any{"enable": "yes"})
	body := toolText(got)
	if !strings.Contains(body, "boolean") {
		t.Errorf("expected type error, got %q", body)
	}
}

func TestDumpsRecent_RejectsBadCtx(t *testing.T) {
	got, _ := execDumpsRecent(map[string]any{"ctx": "queue"})
	body := toolText(got)
	if !strings.Contains(body, `"fpm"`) {
		t.Errorf("expected ctx validation message, got %q", body)
	}
}

// toolText extracts the text payload from a tool response without enforcing
// schema (handles both OK and error shapes).
func toolText(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	c, ok := m["content"].([]map[string]any)
	if !ok {
		return ""
	}
	if len(c) == 0 {
		return ""
	}
	t, _ := c[0]["text"].(string)
	return t
}
