package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// runLocks holds a per-site mutex so two browser tabs (or the palette + the
// dropdown) can't fire the same destructive command concurrently. Keyed by
// site domain; the value is the locked flag plus a name for debugging.
var (
	runLocksMu sync.Mutex
	runLocks   = map[string]string{} // domain → currently-running command name
)

func tryAcquireRun(domain, name string) (release func(), busyWith string, ok bool) {
	runLocksMu.Lock()
	defer runLocksMu.Unlock()
	if cur, busy := runLocks[domain]; busy {
		return nil, cur, false
	}
	runLocks[domain] = name
	return func() {
		runLocksMu.Lock()
		delete(runLocks, domain)
		runLocksMu.Unlock()
	}, "", true
}

// commandRoute dispatches the two commands subroutes:
//
//	GET  /api/sites/{domain}/commands              → list
//	POST /api/sites/{domain}/commands/{name}/run   → execute + stream
//
// Returns true if the request was a commands subroute (handled here), false
// otherwise so the caller can fall through to the generic site action handler.
func commandRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) == 0 || rest[0] != "commands" {
		return false
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, map[string]any{"error": "site not found: " + domain})
		return true
	}
	switch {
	case len(rest) == 1 && r.Method == http.MethodGet:
		// List is read-only and safe to expose to LAN viewers.
		handleCommandsList(w, r, site)
	case len(rest) == 3 && rest[2] == "run" && r.Method == http.MethodPost:
		// Run executes arbitrary shell as the lerd-ui user. Loopback-only
		// so a LAN client can't trigger commands on the host.
		if !isLoopbackRequest(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return true
		}
		handleCommandRun(w, r, site, rest[1])
	default:
		http.NotFound(w, r)
	}
	return true
}

func handleCommandsList(w http.ResponseWriter, r *http.Request, site *config.Site) {
	branch := r.URL.Query().Get("branch")
	cmds := resolveSiteCommands(site, branch)
	// A project-supplied command that hasn't been approved yet runs on the host,
	// so force the confirm modal (which shows the command) until the user approves
	// it once. ProjectOrigin itself is not serialized; the UI only sees Confirm.
	for i := range cmds {
		if cmds[i].ProjectOrigin && !site.CommandApproved(cmds[i].Command) {
			cmds[i].Confirm = true
		}
	}
	writeJSON(w, map[string]any{"commands": cmds})
}

// resolveSiteCommands merges the framework's command set with the project's
// .lerd.yaml entries. When `branch` is non-empty, resolves from the
// worktree's path so the worktree's .lerd.yaml overrides (or extras)
// take precedence over the main checkout's.
func resolveSiteCommands(site *config.Site, branch string) []config.FrameworkCommand {
	if site == nil {
		return nil
	}
	path := site.Path
	if branch != "" {
		if wt := resolveSitePath(site, branch); wt != "" {
			path = wt
		}
	}
	fw, _ := config.GetFrameworkForDir(site.Framework, path)
	proj, _ := config.LoadProjectConfig(path)
	return config.ResolveCommands(fw, proj, path)
}

// urlRegex matches the first http/https URL in stdout, used when the command
// declares output: url (e.g. drush uli, wp user create-session-token).
var urlRegex = regexp.MustCompile(`https?://[^\s'"]+`)

// handleCommandRun executes the named command in the site's project directory
// and streams stdout+stderr to the client as Server-Sent Events. Frames:
//
//	event: stdout    data: <single line of output>
//	event: stderr    data: <single line of output>
//	event: done      data: {"exit": 0, "durationMs": 1234, "url": "..."}
//	event: error     data: <message>     (when setup fails before exec)
//
// stderr is interleaved into the same stream so the UI can render a unified
// terminal view, with a separate event type if we later want to colour it.
func handleCommandRun(w http.ResponseWriter, r *http.Request, site *config.Site, name string) {
	branch := r.URL.Query().Get("branch")
	cmds := resolveSiteCommands(site, branch)
	var target *config.FrameworkCommand
	for i := range cmds {
		if cmds[i].Name == name {
			target = &cmds[i]
			break
		}
	}
	if target == nil {
		writeJSON(w, map[string]any{"error": "command not found: " + name})
		return
	}
	if target.Command == "" {
		writeJSON(w, map[string]any{"error": "command has no shell invocation"})
		return
	}

	// A project-supplied command runs on the host. Require the user to have
	// approved this exact command (the confirm modal posts approve=1) before
	// running it; trusted framework commands are unaffected.
	if target.ProjectOrigin {
		allowed, disabled := config.HostCommandAllowed(site.Name, target.Command)
		switch {
		case disabled:
			writeJSON(w, map[string]any{"error": "project-supplied host commands are disabled (host_commands.disabled)"})
			return
		case !allowed && r.URL.Query().Get("approve") != "1":
			writeJSON(w, map[string]any{"needsConfirm": true, "command": target.Command})
			return
		case !allowed:
			_ = config.ApproveSiteCommand(site.Name, target.Command)
		}
	}

	// Per-site mutex: refuse if another command is already running on this
	// site. Prevents two tabs (or palette + dropdown) from concurrently
	// hammering migrate:fresh, etc. Prefer site.Name, fall back to the
	// first domain, then the project path so we always have a unique key.
	lockKey := site.Name
	if lockKey == "" && len(site.Domains) > 0 {
		lockKey = site.Domains[0]
	}
	if lockKey == "" {
		lockKey = site.Path
	}
	release, busyWith, ok := tryAcquireRun(lockKey, target.Name)
	if !ok {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]any{"error": "another command is already running on this site: " + busyWith})
		return
	}
	defer release()

	// Worktree branch must resolve; falling back to site.Path would point
	// destructive commands (migrate:fresh) at the main DB.
	basePath := site.Path
	if branch != "" {
		wt := resolveSitePath(site, branch)
		if wt == "" {
			writeJSON(w, map[string]any{"error": "unknown worktree branch: " + branch})
			return
		}
		basePath = wt
	}
	cwd := basePath
	if target.CWD != "" && target.CWD != "." {
		cwd = filepath.Join(basePath, target.CWD)
	}

	// Terminal mode: spawn the user's terminal emulator with the command
	// running inside, then return immediately. The UI handles this by
	// skipping the modal and showing a toast.
	if target.Output == config.CommandOutputTerminal {
		script := "cd " + podman.ShellQuote(cwd) + " && " + target.Command + "\nprintf '\\n[press any key to close]'\nread -n 1 -s -r 2>/dev/null || read"
		if err := openTerminalCommand(script); err != nil {
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"terminal": true})
		return
	}

	streamShellRun(w, r.Context(), cwd, target.Command, target.Output == config.CommandOutputURL)
}

// streamShellRun execs shell in cwd and streams stdout/stderr to the client as
// SSE (event: stdout|stderr), closing with a done frame carrying the exit code,
// duration, and (when captureURL) a URL parsed from the output. Shared by the
// command runner and the doctor fix runner so both produce an identical stream.
func streamShellRun(w http.ResponseWriter, ctx context.Context, cwd, shell string, captureURL bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, map[string]any{"error": "streaming not supported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx proxy buffering
	w.WriteHeader(http.StatusOK)

	// writeMu serializes SSE frame writes and `captured` appends across the
	// two pipe-reader goroutines. http.ResponseWriter and strings.Builder
	// are not safe for concurrent use; without this lock the bytes from
	// stdout and stderr can interleave inside a frame and corrupt the
	// stream (caught by `go test -race`).
	var writeMu sync.Mutex

	send := func(event, data string) {
		// Each non-empty data line must be prefixed; multi-line bodies use
		// multiple `data:` lines per the SSE spec.
		var b strings.Builder
		b.WriteString("event: ")
		b.WriteString(event)
		b.WriteByte('\n')
		for _, line := range strings.Split(data, "\n") {
			b.WriteString("data: ")
			b.WriteString(line)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
		writeMu.Lock()
		defer writeMu.Unlock()
		_, _ = io.WriteString(w, b.String())
		flusher.Flush()
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", shell)
	cmd.Dir = cwd
	// Prepend BinDir so php/composer/npm shims resolve under launchd's
	// restricted PATH on macOS. Skip the trailing separator when PATH is
	// empty — a bare "PATH=<bin>:" would search CWD on POSIX.
	path := config.BinDir()
	if existing := os.Getenv("PATH"); existing != "" {
		path += string(os.PathListSeparator) + existing
	}
	cmd.Env = append(os.Environ(), "PATH="+path)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		send("error", "stdout pipe: "+err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		send("error", "stderr pipe: "+err.Error())
		return
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		send("error", "start: "+err.Error())
		return
	}

	var captured strings.Builder
	streamPipe := func(pipe io.Reader, event string) {
		s := bufio.NewScanner(pipe)
		s.Buffer(make([]byte, 64*1024), 1024*1024)
		for s.Scan() {
			line := s.Text()
			writeMu.Lock()
			captured.WriteString(line)
			captured.WriteByte('\n')
			writeMu.Unlock()
			send(event, line)
		}
	}

	// defer-recover so a panic in streamPipe still releases the wait;
	// otherwise the handler would hang holding the per-site run lock.
	done := make(chan struct{}, 2)
	pump := func(pipe io.Reader, event string) {
		defer func() {
			_ = recover()
			done <- struct{}{}
		}()
		streamPipe(pipe, event)
	}
	go pump(stdout, "stdout")
	go pump(stderr, "stderr")
	<-done
	<-done

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			send("error", "wait: "+err.Error())
			return
		}
	}

	payload := map[string]any{
		"exit":       exitCode,
		"durationMs": time.Since(start).Milliseconds(),
	}
	if captureURL {
		if u := urlRegex.FindString(captured.String()); u != "" {
			payload["url"] = u
		}
	}
	body, _ := json.Marshal(payload)
	send("done", string(body))
}
