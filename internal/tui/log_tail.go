package tui

import (
	"bufio"
	"context"
	"os/exec"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/geodro/lerd/internal/podman"
)

// LogKind picks between `podman logs` (containers), `journalctl --user`
// (worker systemd units), and `tail -F` (framework app log files).
// Workers don't run as containers so their output lives in the user
// journal; sites and services do, and their history is richer when pulled
// from podman; framework app logs (laravel.log, etc.) live on disk
// because the framework writes them directly.
type LogKind int

const (
	kindPodman LogKind = iota
	kindJournal
	kindFile
)

// LogTarget fully describes one tail-able source: what to tail, how to tail
// it, and how to label it in the pane header.
type LogTarget struct {
	Kind  LogKind
	ID    string
	Label string
}

type logLineMsg struct {
	source string
	line   string
}

type logClosedMsg struct{ source string }

// logTail runs at most one `podman logs -f` subprocess at a time. Switching
// targets cancels the previous one and opens a fresh tail. A bounded ring
// caps memory on chatty containers.
type logTail struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	target LogTarget
	ch     chan string
	lines  []string
	max    int
}

func newLogTail() *logTail { return &logTail{max: 500} }

// Start cancels any prior tail and spawns a fresh one for `target`.
// The returned Cmd waits on the first line so Update can chain Follow().
func (t *logTail) Start(target LogTarget) tea.Cmd {
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	t.lines = nil
	t.target = target
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	ch := make(chan string, 64)
	t.ch = ch
	go t.run(ctx, target, ch)
	t.mu.Unlock()
	return t.readOne(target.ID, ch)
}

func (t *logTail) Stop() {
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.target = LogTarget{}
	t.ch = nil
	t.mu.Unlock()
}

func (t *logTail) Lines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.lines))
	copy(out, t.lines)
	return out
}

// Source returns the raw identifier of the active tail (container or unit
// name). Used by Update to match incoming logLineMsgs against the current
// target so stale reads from a just-cancelled tail are dropped on the floor.
func (t *logTail) Source() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.target.ID
}

// Target returns the full active target for rendering the pane title.
func (t *logTail) Target() LogTarget {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.target
}

func (t *logTail) append(line string) {
	t.mu.Lock()
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
	t.mu.Unlock()
}

func (t *logTail) run(ctx context.Context, target LogTarget, ch chan<- string) {
	defer close(ch)
	var cmd *exec.Cmd
	switch target.Kind {
	case kindJournal:
		cmd = exec.CommandContext(ctx, "journalctl", "--user", "-u", target.ID,
			"-f", "--no-pager", "-n", "200", "--output=cat")
	case kindFile:
		// -F follows by name, re-opening if the file is rotated, which is
		// what Laravel-style loggers do when they roll daily. -n 200 gives
		// the same scrollback as the podman/journal variants.
		cmd = exec.CommandContext(ctx, "tail", "-n", "200", "-F", target.ID)
	default:
		cmd = exec.CommandContext(ctx, podman.PodmanBin(), "logs", "-f", "--tail", "200", target.ID)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return
		case ch <- scanner.Text():
		}
	}
	_ = cmd.Wait()
}

func (t *logTail) readOne(source string, ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return logClosedMsg{source: source}
		}
		t.append(line)
		return logLineMsg{source: source, line: line}
	}
}

// Follow returns a Cmd that reads the next line from the currently-active
// tail. Returns nil when no tail is running.
func (t *logTail) Follow() tea.Cmd {
	t.mu.Lock()
	source := t.target.ID
	ch := t.ch
	t.mu.Unlock()
	if source == "" || ch == nil {
		return nil
	}
	return t.readOne(source, ch)
}
