package node

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/config"
)

// JSRuntime returns the explicit per-project JS runtime override from
// .lerd.yaml's js_runtime field, normalized to "bun", "node", or "" (unset /
// unrecognized, meaning auto-detect). Node aliases (node/nodejs/npm) all map to
// "node" so a small typo doesn't silently defeat the override and re-force bun.
func JSRuntime(dir string) string {
	raw := ""
	if cfg, err := config.LoadProjectConfig(dir); err == nil && cfg != nil {
		raw = strings.ToLower(strings.TrimSpace(cfg.JSRuntime))
	}
	switch raw {
	case "bun":
		return "bun"
	case "node", "nodejs", "npm":
		return "node"
	default:
		return ""
	}
}

// UsesBun reports whether the project in dir should run its JS tooling through
// bun instead of npm. The .lerd.yaml js_runtime override wins ("bun" forces
// bun, "node"/"npm" forces Node); otherwise lerd auto-detects bun from a
// bun.lockb / bun.lock / bunfig.toml file or a packageManager: bun field. (When
// no Node is available at all, the host-worker path falls back to bun unless
// js_runtime pins Node; see bunRunnerFor.)
func UsesBun(dir string) bool {
	switch JSRuntime(dir) {
	case "bun":
		return true
	case "node":
		return false
	}
	for _, f := range []string{"bun.lockb", "bun.lock", "bunfig.toml"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}

	pkgJSON := filepath.Join(dir, "package.json")
	if data, err := os.ReadFile(pkgJSON); err == nil {
		var pkg struct {
			PackageManager string `json:"packageManager"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			return strings.HasPrefix(strings.TrimSpace(pkg.PackageManager), "bun")
		}
	}
	return false
}

// BunPath resolves the host bun binary: the official installer drops it in
// ~/.bun/bin, which is not on the controlled PATH lerd gives host workers, so
// check there first and fall back to PATH. Returns "" when bun isn't installed.
func BunPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".bun", "bin", "bun")
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("bun"); err == nil {
		return p
	}
	// Homebrew installs bun outside the lerd-watcher daemon's restricted PATH
	// on macOS; check the standard prefixes before giving up.
	if runtime.GOOS == "darwin" {
		for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
			p := filepath.Join(dir, "bun")
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p
			}
		}
	}
	return ""
}

var (
	bunVerMu  sync.Mutex
	bunVerVal string
	bunVerAt  time.Time
)

// BunVersion returns the host bun version (e.g. "1.3.14"), or "" when bun isn't
// installed or doesn't run. The exec result is cached for 30s because callers
// like the UI status snapshot rebuild on every poll and WebSocket push; the
// version rarely changes within that window (`bun upgrade` reflects on the next
// refresh).
func BunVersion() string {
	bunVerMu.Lock()
	defer bunVerMu.Unlock()
	if !bunVerAt.IsZero() && time.Since(bunVerAt) < 30*time.Second {
		return bunVerVal
	}
	bunVerVal = ""
	if bun := BunPath(); bun != "" {
		if out, err := exec.Command(bun, "--version").Output(); err == nil {
			bunVerVal = strings.TrimSpace(string(out))
		}
	}
	bunVerAt = time.Now()
	return bunVerVal
}

// SystemNodeAvailable reports whether a `node` binary is resolvable on PATH
// (outside lerd's own fnm shims). Used to decide the bun fallback and to
// surface the active JS runtime in the UI.
func SystemNodeAvailable() bool {
	_, err := exec.LookPath("node")
	return err == nil
}

// Bunify rewrites the npm/npx/node command verb to its bun equivalent
// (npm->bun, npx->bunx, node->bun), leaving the rest verbatim. It first walks
// past a leading `env` invocation and any KEY=VALUE assignments, since host-proxy
// commands are wrapped as `env PORT=N npm run ...` and would otherwise never
// switch. Commands whose verb isn't npm/npx/node are returned unchanged.
func Bunify(command string) string {
	pos := 0
	for {
		ws := 0
		for pos+ws < len(command) && (command[pos+ws] == ' ' || command[pos+ws] == '\t') {
			ws++
		}
		start := pos + ws
		end := start
		for end < len(command) && command[end] != ' ' && command[end] != '\t' {
			end++
		}
		tok := command[start:end]
		if tok == "" {
			return command
		}
		// Skip a leading `env` and env assignments (KEY=VALUE), but only when
		// there's another token after them to rewrite.
		if (tok == "env" || isEnvAssignment(tok)) && end < len(command) {
			pos = end
			continue
		}
		var repl string
		switch tok {
		case "npm", "node":
			repl = "bun"
		case "npx":
			repl = "bunx"
		default:
			return command
		}
		return command[:start] + repl + command[end:]
	}
}

// isEnvAssignment reports whether tok looks like a shell env assignment
// (NAME=value with a valid identifier before the first '=').
func isEnvAssignment(tok string) bool {
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return false
	}
	for i := 0; i < eq; i++ {
		c := tok[i]
		switch {
		case c == '_', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}
