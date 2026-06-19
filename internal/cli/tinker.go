package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/agentenv"
	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
)

// psyshEvalLocRe matches the trailing ` // vendor/psy/psysh/.../eval()'d code:N`
// annotation that Laravel's `dump()` appends when called from psysh, which
// is source-location noise from inside the REPL itself.
var psyshEvalLocRe = regexp.MustCompile(` // vendor/psy/psysh/[^\s]+\(\d+\) : eval\(\)'d code:\d+`)

// tinkerAliasNoticeRe matches the `[!] Aliasing 'X' to 'Y' for this Tinker
// session.` lines that artisan tinker emits whenever a model alias is first
// used. They're internal REPL chatter, not user output.
var tinkerAliasNoticeRe = regexp.MustCompile(`(?m)^\[!\] Aliasing '[^']+' to '[^']+' for this Tinker session\.\s*\n?`)

// tinkerMemoryLimit overrides PHP's 128M CLI default. Laravel's tinker
// boot path (ClassAliasAutoloader requires the full composer class map)
// blows past the default on any non-trivial project. 512M leaves enough
// headroom for medium projects with fat vendor trees while still surfacing
// runaway code (deep recursion, accidental N+1 over a large table).
const tinkerMemoryLimit = "512M"

func cleanTinkerOutput(s string) string {
	// Split on the multi-statement separator first so per-chunk regexes that
	// rely on `^` (start-of-line) also match notices that landed at the
	// start of a non-first chunk.
	chunks := strings.Split(s, TinkerOutputSeparator)
	for i, c := range chunks {
		c = psyshEvalLocRe.ReplaceAllString(c, "")
		c = tinkerAliasNoticeRe.ReplaceAllString(c, "")
		chunks[i] = c
	}
	return strings.Join(chunks, TinkerOutputSeparator)
}

type TinkerResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Mode       string `json:"mode"`
}

// RunTinker evaluates user PHP code inside the site's PHP container and
// captures stdout/stderr. Mode is driven by the framework definition's
// `tinker:` block when present, with a plain-`php` fallback. The mode
// label returned in the response is the framework name (e.g. "laravel")
// or "php" for the fallback.
//
// siteName + branch are forwarded to the container as LERD_SITE / LERD_BRANCH
// env vars so the debug bridge tags `dump()` / `dd()` events with the same
// identifiers FPM requests use (otherwise tinker dumps land under the
// worktree's directory basename rather than the parent site).
func RunTinker(ctx context.Context, sitePath, siteName, branch, code string) (TinkerResult, error) {
	res := TinkerResult{}
	if strings.TrimSpace(code) == "" {
		return res, fmt.Errorf("code is empty")
	}

	version, err := phpDet.DetectVersion(sitePath)
	if err != nil {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			return res, fmt.Errorf("cannot detect PHP version: %w", err)
		}
		version = cfg.PHP.DefaultVersion
	}
	container := fpmContainerForDir(sitePath, version)

	if running, _ := podman.ContainerRunning(container); !running {
		return res, fmt.Errorf("PHP %s FPM container is not running", version)
	}

	podman.EnsurePathMounted(sitePath, version)
	ensureServicesForCwd(sitePath)

	tinkerSpec := config.GetTinkerForDir(sitePath)
	mode := "php"
	if tinkerSpec != nil {
		if site, err := config.FindSiteByPath(sitePath); err == nil && site.Framework != "" {
			mode = site.Framework
		} else {
			mode = "tinker"
		}
	}
	res.Mode = mode

	home := os.Getenv("HOME")
	composerHome := os.Getenv("COMPOSER_HOME")
	if composerHome == "" {
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(home, ".config")
		}
		composerHome = filepath.Join(xdgConfig, "composer")
	}

	dumpFn := detectDumpFunction(sitePath, mode)
	envArgs := tinkerEnvArgs(sitePath, home, composerHome)
	// Forward AI agent detection vars so agent-detector (e.g. laravel/pao)
	// still emits JSON when run inside the container.
	for _, e := range agentenv.Passthrough(os.Environ()) {
		envArgs = append(envArgs, "--env", e)
	}
	if siteName != "" {
		envArgs = append(envArgs, "--env", "LERD_SITE="+siteName)
	}
	if branch != "" {
		envArgs = append(envArgs, "--env", "LERD_BRANCH="+branch)
	}

	var argv []string
	var stdinPipe string
	if tinkerSpec != nil {
		// Wrap each top-level statement so its output is delimited by
		// TinkerOutputSeparator, and auto-dump bare expressions so
		// `User::count()` shows its value. Frontend splits on the
		// separator to render one block per statement.
		payload := transformForMultiStatementWithDump(code, dumpFn)
		argv = append([]string{"exec", "-i", "-w", sitePath}, envArgs...)
		argv = append(argv, container, "php", "-d", "memory_limit="+tinkerMemoryLimit)
		argv = append(argv, tinkerSpec.Command...)
		switch {
		case tinkerSpec.ExecuteFlag != "":
			argv = append(argv, tinkerSpec.ExecuteFlag+"="+payload)
		case tinkerSpec.ExecutePositional:
			argv = append(argv, payload)
		default:
			stdinPipe = payload
		}
	} else {
		// Plain PHP fallback: write a temp script, autoload composer,
		// dump-or-var_dump bare expressions for visible output.
		payload := transformForMultiStatementWithDump(code, dumpFn)
		tmpFile, err := writeTinkerScript(sitePath, payload, mode)
		if err != nil {
			return res, fmt.Errorf("writing tinker script: %w", err)
		}
		defer os.Remove(tmpFile)
		argv = append([]string{"exec", "-i", "-w", sitePath}, envArgs...)
		argv = append(argv, container, "php", "-d", "memory_limit="+tinkerMemoryLimit, tmpFile)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, podman.PodmanBin(), argv...)
	if stdinPipe != "" {
		cmd.Stdin = strings.NewReader(stdinPipe)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	res.DurationMs = time.Since(start).Milliseconds()
	res.Stdout = cleanTinkerOutput(stdout.String())
	res.Stderr = cleanTinkerOutput(stderr.String())

	if exit, ok := runErr.(*exec.ExitError); ok {
		res.ExitCode = exit.ExitCode()
		return res, nil
	}
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return res, fmt.Errorf("tinker timed out after %dms", res.DurationMs)
		}
		return res, runErr
	}
	return res, nil
}

// tinkerEnvArgs builds the shared `--env KEY=VAL` argv chunks used by
// every tinker exec invocation: HOME, COMPOSER_HOME, PATH (so vendor/bin
// shims work inside the container), TERM/NO_COLOR so dump output is not
// ANSI-colored, PSYSH_TRUST_PROJECT so PsySH skips its non-interactive
// "Restricted Mode" warning (the user is running their own project code in
// their own container; restricting it adds noise without security gain),
// and LERD_DUMP_PASSTHROUGH=1 so when the debug bridge is on the auto-
// wrapped `dump(expr)` still prints to stdout. Without it the bridge
// silently swallows the value and the REPL shows nothing for bare
// expressions like `User::count()`.
func tinkerEnvArgs(sitePath, home, composerHome string) []string {
	projectVendorBin := filepath.Join(sitePath, "vendor", "bin")
	composerBin := filepath.Join(composerHome, "vendor", "bin")
	return []string{
		"--env", "HOME=" + home,
		"--env", "COMPOSER_HOME=" + composerHome,
		"--env", "PATH=" + projectVendorBin + ":/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:" + composerBin,
		"--env", "NO_COLOR=1",
		"--env", "TERM=dumb",
		"--env", "PSYSH_TRUST_PROJECT=1",
		"--env", "LERD_DUMP_PASSTHROUGH=1",
	}
}

// TinkerOutputSeparator is the marker we inject between top-level statements
// so the frontend can split a single tinker run into one output block per
// statement. ASCII 0x1e (record separator) — unlikely to appear in user
// output, easy to escape inside a PHP double-quoted string as `\x1e`.
const TinkerOutputSeparator = "\x1e"

// transformForMultiStatement keeps the legacy callsite working with the
// default Laravel `dump()` helper. Tinker mode uses this directly.
func transformForMultiStatement(code string) string {
	return transformForMultiStatementWithDump(code, "dump")
}

// transformForMultiStatementWithDump is the parameterized variant that
// lets callers pick which dump function to wrap bare expressions with —
// `dump` for Symfony VarDumper / Laravel, `var_dump` for vanilla PHP.
func transformForMultiStatementWithDump(code, dumpFn string) string {
	return transformWithSeparator(code, func(s string) string {
		return autoDumpLastExpressionWith(s, dumpFn)
	})
}

func transformWithSeparator(code string, wrap func(string) string) string {
	parts := splitTopLevelStatements(code)
	if len(parts) == 0 {
		return code
	}
	if len(parts) == 1 {
		return wrap(strings.TrimSpace(code))
	}

	var sb strings.Builder
	written := 0
	for _, p := range parts {
		body := strings.TrimSpace(p)
		if body == "" {
			continue
		}
		if written > 0 {
			sb.WriteString(` echo "\x1e";`)
		}
		out := wrap(body)
		sb.WriteString(out)
		if !strings.HasSuffix(strings.TrimRight(out, " \t\n"), ";") {
			sb.WriteString(";")
		}
		written++
	}
	return sb.String()
}

// detectDumpFunction picks which PHP function we should wrap bare
// expressions in to surface their values. Laravel sites always have
// `dump()` via the framework. Symfony/plain PHP get `dump()` if Symfony
// VarDumper is in vendor (any Symfony skeleton has it), else
// `var_dump()` which is always available. Mode "php" means no
// framework REPL was selected.
func detectDumpFunction(sitePath, mode string) string {
	if mode != "php" {
		return "dump"
	}
	if fileExists(filepath.Join(sitePath, "vendor", "symfony", "var-dumper")) {
		return "dump"
	}
	return "var_dump"
}

// splitTopLevelStatements splits `code` at top-level semicolons, respecting
// string literals and nested brackets. The returned slice contains each
// statement's text without the trailing `;`.
func splitTopLevelStatements(code string) []string {
	var parts []string
	var cur strings.Builder
	depth := 0
	inSingle, inDouble := false, false
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case inSingle:
			cur.WriteByte(c)
			if c == '\\' && i+1 < len(code) {
				cur.WriteByte(code[i+1])
				i++
				continue
			}
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			cur.WriteByte(c)
			if c == '\\' && i+1 < len(code) {
				cur.WriteByte(code[i+1])
				i++
				continue
			}
			if c == '"' {
				inDouble = false
			}
		case c == '\'':
			inSingle = true
			cur.WriteByte(c)
		case c == '"':
			inDouble = true
			cur.WriteByte(c)
		case c == '(' || c == '[' || c == '{':
			depth++
			cur.WriteByte(c)
		case c == ')' || c == ']' || c == '}':
			if depth > 0 {
				depth--
			}
			cur.WriteByte(c)
		case c == ';' && depth == 0:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		parts = append(parts, cur.String())
	}
	return parts
}

// autoDumpLastExpression wraps the user's code in `dump(...)` when it's a
// single bare expression, so `User::count()` shows its value at the REPL.
// See autoDumpLastExpressionWith for the parameterized version.
func autoDumpLastExpression(code string) string {
	return autoDumpLastExpressionWith(code, "dump")
}

// autoDumpLastExpressionWith wraps the user's code in `<dumpFn>(...)` when
// it's a single bare expression. Falls back to the original unchanged
// when the code is multi-statement, starts with a control/output keyword,
// or already calls a known dump function.
func autoDumpLastExpressionWith(code, dumpFn string) string {
	trimmed := strings.TrimSpace(code)
	trimmed = strings.TrimRight(trimmed, ";")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return code
	}
	if hasTopLevelSemicolon(trimmed) {
		return code
	}
	if startsWithNonExprKeyword(trimmed) {
		return code
	}
	if startsWithDumpCall(trimmed) {
		return code
	}
	return dumpFn + "(" + trimmed + ");"
}

// hasTopLevelSemicolon returns true if `s` contains a `;` outside of any
// string literal, comment, or matching bracket. A naive check that handles
// the common single-line REPL case; multi-line blocks with `;` inside
// strings will fall through to "multi-statement" treatment, which is fine
// (we just don't auto-wrap).
func hasTopLevelSemicolon(s string) bool {
	depth := 0
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == '"' {
				inDouble = false
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth > 0 {
				depth--
			}
		case c == ';' && depth == 0:
			return true
		}
	}
	return false
}

var nonExprKeywords = []string{
	"echo", "print", "var_dump", "dump", "dd", "ddd", "return",
	"throw", "if", "else", "elseif", "while", "do", "for", "foreach",
	"switch", "function", "class", "interface", "trait", "abstract",
	"final", "use", "namespace", "include", "require", "include_once",
	"require_once", "unset", "goto", "global", "static", "yield", "match",
	"try", "catch", "finally", "declare", "break", "continue",
}

func startsWithNonExprKeyword(s string) bool {
	lower := strings.ToLower(s)
	for _, kw := range nonExprKeywords {
		if lower == kw {
			return true
		}
		if strings.HasPrefix(lower, kw) {
			rest := lower[len(kw):]
			if rest == "" {
				return true
			}
			c := rest[0]
			if c == ' ' || c == '\t' || c == '\n' || c == '(' || c == '{' || c == ';' {
				return true
			}
		}
	}
	return false
}

func startsWithDumpCall(s string) bool {
	for _, fn := range []string{"dump(", "dd(", "ddd(", "var_dump(", "print_r("} {
		if strings.HasPrefix(s, fn) {
			return true
		}
	}
	return false
}

// writeTinkerScript writes the user's PHP code to a temp file inside the
// site directory (so it's visible from inside the container). Only used
// by plain-PHP mode; tinker mode pipes via stdin. If composer's autoload
// exists, we require it so helpers like Symfony's `dump()` and any
// project class are available.
func writeTinkerScript(sitePath, code, mode string) (string, error) {
	_ = mode
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	name := ".lerd-tinker-" + hex.EncodeToString(buf) + ".php"
	full := filepath.Join(sitePath, name)

	body := strings.TrimLeft(code, " \t\r\n")
	hasOpenTag := strings.HasPrefix(body, "<?php") || strings.HasPrefix(body, "<?=")
	if hasOpenTag {
		// Strip the user's opening tag so we can prepend our own bootstrap.
		nl := strings.IndexByte(body, '\n')
		if nl >= 0 {
			body = body[nl+1:]
		} else {
			body = ""
		}
	}

	prelude := "<?php\n"
	if fileExists(filepath.Join(sitePath, "vendor", "autoload.php")) {
		prelude += "require __DIR__ . '/vendor/autoload.php';\n"
	}

	body = prelude + body
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(full, []byte(body), 0644); err != nil {
		return "", err
	}
	return full, nil
}
