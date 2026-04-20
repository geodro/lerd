package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteGlobalAISkills_writesAllThreeFiles(t *testing.T) {
	home := t.TempDir()

	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("WriteGlobalAISkills: %v", err)
	}

	expect := []string{
		filepath.Join(home, ".claude", "skills", "lerd", "SKILL.md"),
		filepath.Join(home, ".cursor", "rules", "lerd.mdc"),
		filepath.Join(home, ".junie", "guidelines.md"),
	}
	for _, path := range expect {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", path)
		}
	}

	skill, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "lerd", "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if string(skill) != claudeSkillContent {
		t.Errorf("SKILL.md content does not match embedded claudeSkillContent")
	}

	rules, err := os.ReadFile(filepath.Join(home, ".cursor", "rules", "lerd.mdc"))
	if err != nil {
		t.Fatalf("read lerd.mdc: %v", err)
	}
	if string(rules) != cursorRulesContent {
		t.Errorf("lerd.mdc content does not match embedded cursorRulesContent")
	}

	guidelines, err := os.ReadFile(filepath.Join(home, ".junie", "guidelines.md"))
	if err != nil {
		t.Fatalf("read guidelines.md: %v", err)
	}
	if !strings.Contains(string(guidelines), "<!-- lerd:begin -->") {
		t.Errorf("guidelines.md missing lerd block sentinel")
	}
	if !strings.Contains(string(guidelines), "<!-- lerd:end -->") {
		t.Errorf("guidelines.md missing lerd end sentinel")
	}
}

func TestWriteGlobalAISkills_idempotent(t *testing.T) {
	home := t.TempDir()

	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("second call: %v", err)
	}

	guidelines, err := os.ReadFile(filepath.Join(home, ".junie", "guidelines.md"))
	if err != nil {
		t.Fatalf("read guidelines: %v", err)
	}
	if got := strings.Count(string(guidelines), "<!-- lerd:begin -->"); got != 1 {
		t.Errorf("expected 1 lerd:begin sentinel, got %d", got)
	}
	if got := strings.Count(string(guidelines), "<!-- lerd:end -->"); got != 1 {
		t.Errorf("expected 1 lerd:end sentinel, got %d", got)
	}
}

func TestWriteGlobalAISkills_preservesExistingGuidelines(t *testing.T) {
	home := t.TempDir()

	guidelinesPath := filepath.Join(home, ".junie", "guidelines.md")
	if err := os.MkdirAll(filepath.Dir(guidelinesPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "# Project guidelines\n\nFollow house style.\n"
	if err := os.WriteFile(guidelinesPath, []byte(existing), 0644); err != nil {
		t.Fatalf("seed guidelines: %v", err)
	}

	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("WriteGlobalAISkills: %v", err)
	}

	got, err := os.ReadFile(guidelinesPath)
	if err != nil {
		t.Fatalf("read guidelines: %v", err)
	}
	if !strings.Contains(string(got), "Follow house style.") {
		t.Errorf("existing guidelines content was dropped")
	}
	if !strings.Contains(string(got), "<!-- lerd:begin -->") {
		t.Errorf("lerd block not appended")
	}
}

func TestMcpEnabledGlobally_noMarkers(t *testing.T) {
	home := t.TempDir()
	if mcpEnabledGlobally(home) {
		t.Errorf("expected false when no markers present")
	}
}

func TestMcpEnabledGlobally_detectsClaudeSkill(t *testing.T) {
	home := t.TempDir()
	skill := filepath.Join(home, ".claude", "skills", "lerd", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(skill, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !mcpEnabledGlobally(home) {
		t.Errorf("expected true when SKILL.md marker exists")
	}
}

func TestMcpEnabledGlobally_detectsCursorRules(t *testing.T) {
	home := t.TempDir()
	rules := filepath.Join(home, ".cursor", "rules", "lerd.mdc")
	if err := os.MkdirAll(filepath.Dir(rules), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(rules, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !mcpEnabledGlobally(home) {
		t.Errorf("expected true when lerd.mdc marker exists")
	}
}

func TestWriteGlobalAISkills_replacesExistingLerdBlock(t *testing.T) {
	home := t.TempDir()

	guidelinesPath := filepath.Join(home, ".junie", "guidelines.md")
	if err := os.MkdirAll(filepath.Dir(guidelinesPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := "# guidelines\n\n<!-- lerd:begin -->\nstale lerd content\n<!-- lerd:end -->\n"
	if err := os.WriteFile(guidelinesPath, []byte(stale), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := WriteGlobalAISkills(home, false); err != nil {
		t.Fatalf("WriteGlobalAISkills: %v", err)
	}

	got, err := os.ReadFile(guidelinesPath)
	if err != nil {
		t.Fatalf("read guidelines: %v", err)
	}
	if strings.Contains(string(got), "stale lerd content") {
		t.Errorf("stale lerd block was not replaced")
	}
	if !strings.Contains(string(got), "Lerd — Laravel Local Dev Environment") {
		t.Errorf("fresh lerd block not written")
	}
}
