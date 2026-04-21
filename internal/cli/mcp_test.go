package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestProjectHasLerdSkills(t *testing.T) {
	dir := t.TempDir()
	if ProjectHasLerdSkills(dir) {
		t.Fatalf("empty dir should not be opted in")
	}

	skill := filepath.Join(dir, ".claude", "skills", "lerd", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(skill, []byte("x"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !ProjectHasLerdSkills(dir) {
		t.Errorf("SKILL.md presence should signal opt-in")
	}

	dir2 := t.TempDir()
	guidelines := filepath.Join(dir2, ".junie", "guidelines.md")
	if err := os.MkdirAll(filepath.Dir(guidelines), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(guidelines, []byte("header only, no lerd markers\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if ProjectHasLerdSkills(dir2) {
		t.Errorf("guidelines without lerd marker should not signal opt-in")
	}

	if err := os.WriteFile(guidelines, []byte("junk\n<!-- lerd:begin -->\nstuff\n<!-- lerd:end -->\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !ProjectHasLerdSkills(dir2) {
		t.Errorf("guidelines with lerd marker should signal opt-in")
	}
}

func TestWriteProjectAISkills_writesAllArtefacts(t *testing.T) {
	dir := t.TempDir()
	if err := WriteProjectAISkills(dir, false); err != nil {
		t.Fatalf("WriteProjectAISkills: %v", err)
	}

	want := []string{
		".mcp.json",
		".cursor/mcp.json",
		".ai/mcp/mcp.json",
		".junie/mcp/mcp.json",
		".claude/skills/lerd/SKILL.md",
		".cursor/rules/lerd.mdc",
		".junie/guidelines.md",
	}
	for _, rel := range want {
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Errorf("missing %s: %v", rel, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", rel)
		}
	}
	if !ProjectHasLerdSkills(dir) {
		t.Errorf("ProjectHasLerdSkills should return true after WriteProjectAISkills")
	}
}

func TestWriteProjectAISkills_skipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WriteProjectAISkills(dir, false); err != nil {
		t.Fatalf("first call: %v", err)
	}

	skill := filepath.Join(dir, ".claude", "skills", "lerd", "SKILL.md")
	rules := filepath.Join(dir, ".cursor", "rules", "lerd.mdc")

	oldSkillMtime := mtimeOrFail(t, skill)
	oldRulesMtime := mtimeOrFail(t, rules)

	time.Sleep(10 * time.Millisecond)

	if err := WriteProjectAISkills(dir, false); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if got := mtimeOrFail(t, skill); !got.Equal(oldSkillMtime) {
		t.Errorf("SKILL.md was rewritten despite unchanged content (mtime changed from %v to %v)", oldSkillMtime, got)
	}
	if got := mtimeOrFail(t, rules); !got.Equal(oldRulesMtime) {
		t.Errorf("lerd.mdc was rewritten despite unchanged content (mtime changed from %v to %v)", oldRulesMtime, got)
	}
}

func TestWriteProjectAISkills_rewritesWhenContentChanges(t *testing.T) {
	dir := t.TempDir()
	skill := filepath.Join(dir, ".claude", "skills", "lerd", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(skill, []byte("stale content, older schema"), 0644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	if err := WriteProjectAISkills(dir, false); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	got, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != claudeSkillContent {
		t.Errorf("stale SKILL.md was not refreshed")
	}
}

func mtimeOrFail(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime()
}
