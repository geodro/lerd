package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/term"
)

// BuildJob is a labeled build task that writes its output to the provided writer.
type BuildJob struct {
	Label string
	Run   func(w io.Writer) error
}

// RunParallel executes all jobs concurrently with a compact spinner UI.
// In a non-TTY environment it falls back to plain sequential output.
// Returns the first non-nil error, or nil if all jobs succeed.
func RunParallel(jobs []BuildJob) error {
	if len(jobs) == 0 {
		return nil
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return runSequential(jobs)
	}
	return runParallelTUI(jobs)
}

func runSequential(jobs []BuildJob) error {
	var firstErr error
	for _, job := range jobs {
		fmt.Printf("==> %s\n", job.Label)
		if err := job.Run(os.Stdout); err != nil {
			fmt.Printf("  [WARN] %s: %v\n", job.Label, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

type jobState struct {
	label string
	start time.Time
	mu    sync.Mutex
	buf   bytes.Buffer
	end   time.Time
	done  bool
	err   error
}

func (s *jobState) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *jobState) finish(err error) {
	s.mu.Lock()
	s.end = time.Now()
	s.done = true
	s.err = err
	s.mu.Unlock()
}

func (s *jobState) snapshot() (done bool, err error, end time.Time, out []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out = make([]byte, s.buf.Len())
	copy(out, s.buf.Bytes())
	return s.done, s.err, s.end, out
}

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

func runParallelTUI(jobs []BuildJob) error {
	states := make([]*jobState, len(jobs))
	for i, job := range jobs {
		states[i] = &jobState{label: job.Label, start: time.Now()}
	}

	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		go func(idx int, j BuildJob) {
			defer wg.Done()
			states[idx].finish(j.Run(states[idx]))
		}(i, job)
	}

	allDone := make(chan struct{})
	go func() { wg.Wait(); close(allDone) }()

	// Ctrl+O toggles output visibility.
	var showOutput atomic.Bool

	// Open /dev/tty directly for keypress reading so this goroutine never
	// touches os.Stdin — subprocesses (e.g. sudo) can then use stdin freely.
	tty, ttyErr := os.OpenFile("/dev/tty", os.O_RDWR, 0)

	// Enter raw terminal mode so we can read single keypresses.
	var restore func()
	if ttyErr == nil {
		if oldState, err := term.MakeRaw(int(tty.Fd())); err == nil {
			restore = func() {
				term.Restore(int(tty.Fd()), oldState) //nolint:errcheck
				tty.Close()
			}
		} else {
			restore = func() { tty.Close() }
		}
	} else {
		restore = func() {}
	}

	// Handle SIGINT / Ctrl+C gracefully.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		restore()
		fmt.Print("\r\n")
		os.Exit(1)
	}()

	// Read single keypresses from /dev/tty. Closing tty (in restore) causes
	// the Read to return an error, cleanly stopping this goroutine.
	go func() {
		if ttyErr != nil {
			return
		}
		b := make([]byte, 1)
		for {
			if _, err := tty.Read(b); err != nil {
				return
			}
			switch b[0] {
			case 0x0F: // Ctrl+O — toggle output
				showOutput.Store(!showOutput.Load())
			case 0x03: // Ctrl+C
				restore()
				fmt.Print("\r\n")
				os.Exit(1)
			}
		}
	}()

	termWidth := 120
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		termWidth = w
	}

	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()

	frame := 0
	prevLines := 0

	render := func(final bool) {
		show := showOutput.Load()

		var sb strings.Builder

		// Erase previous render.
		if prevLines > 0 {
			fmt.Fprintf(&sb, "\033[%dA\033[J", prevLines)
		}
		lines := 0

		for _, s := range states {
			done, err, end, _ := s.snapshot()

			var elapsed time.Duration
			if done {
				elapsed = end.Sub(s.start).Round(time.Second)
			} else {
				elapsed = time.Since(s.start).Round(time.Second)
			}
			var icon string
			switch {
			case done && err != nil:
				icon = "\033[31m✗\033[0m"
			case done:
				icon = "\033[32m✓\033[0m"
			default:
				icon = "\033[33m" + string(spinnerFrames[frame%len(spinnerFrames)]) + "\033[0m"
			}

			if elapsed >= time.Second {
				mins := int(elapsed.Minutes())
				secs := int(elapsed.Seconds()) % 60
				fmt.Fprintf(&sb, "  %s  %-26s  %02d:%02d\r\n", icon, s.label, mins, secs)
			} else {
				fmt.Fprintf(&sb, "  %s  %-26s\r\n", icon, s.label)
			}
			lines++
		}

		if !final {
			hint := "Ctrl+O: show output"
			if show {
				hint = "Ctrl+O: hide output"
			}
			fmt.Fprintf(&sb, "  \033[2m%s\033[0m\r\n", hint)
			lines++
		}

		if show {
			for _, s := range states {
				_, _, _, out := s.snapshot()
				if len(out) == 0 {
					continue
				}
				fmt.Fprintf(&sb, "  \033[2m─── %s ───\033[0m\r\n", s.label)
				lines++
				for _, l := range strings.Split(tailLines(out, 20, termWidth-4), "\n") {
					fmt.Fprintf(&sb, "  %s\r\n", l)
					lines++
				}
			}
		}

		prevLines = lines
		fmt.Print(sb.String())
		frame++
	}

	for {
		select {
		case <-tick.C:
			render(false)
		case <-allDone:
			render(true)
			restore()
			signal.Stop(sigCh)
			fmt.Print("\r\n")

			var firstErr error
			for _, s := range states {
				done, err, _, out := s.snapshot()
				if done && err != nil {
					if firstErr == nil {
						firstErr = err
					}
					fmt.Printf("\033[31m✗ %s:\033[0m %v\n", s.label, err)
					if len(out) > 0 {
						fmt.Printf("  \033[2m─── output ───\033[0m\n%s\n", out)
					}
				}
			}
			return firstErr
		}
	}
}

// StepRunner runs labeled steps sequentially with a compact in-place TUI.
// Each step's output is hidden by default; press Ctrl+O to toggle it.
// Falls back to plain "  --> label ... OK" output when stdout is not a TTY.
type StepRunner struct {
	mu         sync.Mutex
	steps      []*jobState
	showOutput atomic.Bool
	paused     atomic.Bool
	stopRender chan struct{}
	renderDone chan struct{}
	restore    func()
	termWidth  int
	isTTY      bool
}

// NewStepRunner creates and starts a StepRunner.
// Call Close() when all steps are done to restore the terminal.
func NewStepRunner() *StepRunner {
	r := &StepRunner{
		stopRender: make(chan struct{}),
		renderDone: make(chan struct{}),
		termWidth:  120,
		isTTY:      term.IsTerminal(int(os.Stdout.Fd())),
	}
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		r.termWidth = w
	}
	if !r.isTTY {
		r.restore = func() {}
		return r
	}

	if oldState, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		r.restore = func() { term.Restore(int(os.Stdin.Fd()), oldState) } //nolint:errcheck
	} else {
		r.restore = func() {}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		r.restore()
		fmt.Print("\r\n")
		os.Exit(1)
	}()

	go func() {
		b := make([]byte, 1)
		for {
			if _, err := os.Stdin.Read(b); err != nil {
				return
			}
			switch b[0] {
			case 0x0F: // Ctrl+O
				r.showOutput.Store(!r.showOutput.Load())
			case 0x03: // Ctrl+C
				r.restore()
				fmt.Print("\r\n")
				os.Exit(1)
			}
		}
	}()

	go func() {
		defer close(r.renderDone)
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		prevLines, frame := 0, 0
		for {
			select {
			case <-tick.C:
				if !r.paused.Load() {
					prevLines, frame = r.renderSteps(prevLines, frame, false)
				}
			case <-r.stopRender:
				r.renderSteps(prevLines, frame, true)
				return
			}
		}
	}()

	return r
}

// Run executes fn as a labeled step. In TUI mode the step shows a spinner
// while running and ✓/✗ when done. Returns fn's error.
func (r *StepRunner) Run(label string, fn func(w io.Writer) error) error {
	if !r.isTTY {
		fmt.Printf("  --> %s ... ", label)
		err := fn(io.Discard)
		if err != nil {
			fmt.Printf("[WARN: %v]\n", err)
		} else {
			fmt.Println("OK")
		}
		return err
	}

	entry := &jobState{label: label, start: time.Now()}
	r.mu.Lock()
	r.steps = append(r.steps, entry)
	r.mu.Unlock()

	err := fn(entry)
	entry.finish(err)
	return err
}

// RunInteractive temporarily restores the terminal to cooked mode so that steps
// which need interactive sudo (password prompts, etc.) work correctly.
// The spinner pauses, the step runs with full terminal access, then raw mode resumes.
func (r *StepRunner) RunInteractive(label string, fn func() error) error {
	if !r.isTTY {
		fmt.Printf("  --> %s\n", label)
		err := fn()
		if err != nil {
			fmt.Printf("  [WARN: %v]\n", err)
		}
		return err
	}

	// Pause the render loop and give it time to finish its current tick.
	r.paused.Store(true)
	time.Sleep(90 * time.Millisecond)
	r.restore()

	fmt.Printf("  --> %s\n", label)
	err := fn()
	if err != nil {
		fmt.Printf("  [WARN: %v]\n", err)
	}

	// Re-enter raw mode and resume rendering.
	if oldState, rawErr := term.MakeRaw(int(os.Stdin.Fd())); rawErr == nil {
		r.restore = func() { term.Restore(int(os.Stdin.Fd()), oldState) } //nolint:errcheck
	}
	r.paused.Store(false)

	return err
}

// Close stops the render loop, restores the terminal, and prints a final newline.
func (r *StepRunner) Close() {
	if !r.isTTY {
		return
	}
	close(r.stopRender)
	<-r.renderDone
	r.restore()
	fmt.Println()
}

func (r *StepRunner) renderSteps(prevLines, frame int, final bool) (int, int) {
	show := r.showOutput.Load()

	r.mu.Lock()
	steps := make([]*jobState, len(r.steps))
	copy(steps, r.steps)
	r.mu.Unlock()

	var sb strings.Builder
	if prevLines > 0 {
		fmt.Fprintf(&sb, "\033[%dA\033[J", prevLines)
	}
	lines := 0

	for _, s := range steps {
		done, err, end, _ := s.snapshot()
		var elapsed time.Duration
		if done {
			elapsed = end.Sub(s.start).Round(time.Second)
		} else {
			elapsed = time.Since(s.start).Round(time.Second)
		}
		var icon string
		switch {
		case done && err != nil:
			icon = "\033[31m✗\033[0m"
		case done:
			icon = "\033[32m✓\033[0m"
		default:
			icon = "\033[33m" + string(spinnerFrames[frame%len(spinnerFrames)]) + "\033[0m"
		}

		if elapsed >= time.Second {
			mins := int(elapsed.Minutes())
			secs := int(elapsed.Seconds()) % 60
			fmt.Fprintf(&sb, "  %s  %-32s  %02d:%02d\r\n", icon, s.label, mins, secs)
		} else {
			fmt.Fprintf(&sb, "  %s  %-32s\r\n", icon, s.label)
		}
		lines++
	}

	if !final {
		hint := "Ctrl+O: show output"
		if show {
			hint = "Ctrl+O: hide output"
		}
		fmt.Fprintf(&sb, "  \033[2m%s\033[0m\r\n", hint)
		lines++
	}

	if show {
		for _, s := range steps {
			_, _, _, out := s.snapshot()
			if len(out) == 0 {
				continue
			}
			fmt.Fprintf(&sb, "  \033[2m─── %s ───\033[0m\r\n", s.label)
			lines++
			for _, l := range strings.Split(tailLines(out, 20, r.termWidth-4), "\n") {
				fmt.Fprintf(&sb, "  %s\r\n", l)
				lines++
			}
		}
	}

	fmt.Print(sb.String())
	return lines, frame + 1
}

// tailLines returns the last n lines of b, truncated to maxWidth chars each.
func tailLines(b []byte, n, maxWidth int) string {
	clean := strings.ReplaceAll(string(b), "\r", "")
	all := strings.Split(strings.TrimRight(clean, "\n"), "\n")
	if len(all) > n {
		all = all[len(all)-n:]
	}
	for i, l := range all {
		if len(l) > maxWidth {
			all[i] = l[:maxWidth-3] + "..."
		}
	}
	return strings.Join(all, "\n")
}
