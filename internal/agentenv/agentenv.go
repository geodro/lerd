// Package agentenv forwards AI coding-agent detection variables across the
// container boundary so tools like laravel/pao (via laravel/agent-detector)
// still switch to JSON output when run through `lerd php`, tinker, or MCP.
package agentenv

import "strings"

// Vars are the environment variables laravel/agent-detector inspects to
// decide an AI agent is driving the process. Kept in sync with that library's
// AGENT_ENV_VARS set so detection survives `lerd php`, tinker, and MCP exec.
var Vars = []string{
	"AI_AGENT",
	"CLAUDECODE", "CLAUDE_CODE", "CLAUDE_CODE_IS_COWORK",
	"CURSOR_AGENT",
	"GEMINI_CLI",
	"CODEX_SANDBOX", "CODEX_CI", "CODEX_THREAD_ID",
	"AUGMENT_AGENT",
	"AMP_CURRENT_THREAD_ID",
	"OPENCODE_CLIENT", "OPENCODE",
	"COPILOT_MODEL", "COPILOT_ALLOW_ALL", "COPILOT_GITHUB_TOKEN", "COPILOT_CLI",
	"REPL_ID",
	"ANTIGRAVITY_AGENT",
	"PI_CODING_AGENT",
	"KIRO_AGENT_PATH",
}

// presenceOnly holds detection vars that carry secrets. agent-detector only
// checks that they exist, so we forward a placeholder rather than leak the
// real value into the container environment.
var presenceOnly = map[string]bool{
	"COPILOT_GITHUB_TOKEN": true,
}

// Passthrough returns the KEY=VALUE entries from environ for any agent
// detection variable that is set, ready to hand to `--env`. Values are kept
// verbatim (agent-detector pattern-matches some) except for presenceOnly vars.
func Passthrough(environ []string) []string {
	want := make(map[string]bool, len(Vars))
	for _, k := range Vars {
		want[k] = true
	}
	var out []string
	for _, e := range environ {
		k, _, ok := strings.Cut(e, "=")
		if !ok || !want[k] {
			continue
		}
		if presenceOnly[k] {
			out = append(out, k+"=1")
		} else {
			out = append(out, e)
		}
	}
	return out
}
