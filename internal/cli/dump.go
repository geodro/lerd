package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dumps"
	"github.com/geodro/lerd/internal/dumpsops"
	"github.com/geodro/lerd/internal/feedback"
	"github.com/spf13/cobra"
)

// NewDumpCmd returns the parent `lerd dump` command. Subcommands toggle the
// debug bridge, tail received dumps, and inspect state.
func NewDumpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump",
		Short: "Capture PHP dump()/dd() calls into the lerd dashboard",
		Long: `Toggle the lerd debug bridge so that calls to dump() and dd() in your PHP
code ship to the lerd dashboard, TUI, and MCP tools instead of (only) hitting
the response. Off by default — enable with ` + "`lerd dump on`" + ` and disable
with ` + "`lerd dump off`" + `.`,
	}
	cmd.AddCommand(newDumpOnCmd())
	cmd.AddCommand(newDumpOffCmd())
	cmd.AddCommand(newDumpStatusCmd())
	cmd.AddCommand(newDumpTailCmd())
	cmd.AddCommand(newDumpClearCmd())
	return cmd
}

func newDumpOnCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "on",
		Short: "Enable the debug bridge for every installed PHP version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDumpToggle(true)
		},
	}
}

func newDumpOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Disable the debug bridge",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDumpToggle(false)
		},
	}
}

func newDumpStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the debug bridge is enabled and how many dumps are buffered",
		RunE:  runDumpStatus,
	}
}

func newDumpClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear the in-memory dump ring",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDumpClear()
		},
	}
}

func newDumpTailCmd() *cobra.Command {
	var (
		site    string
		branch  string
		ctxKind string
	)
	cmd := &cobra.Command{
		Use:   "tail",
		Short: "Stream dumps to the terminal as they arrive",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDumpTail(site, branch, ctxKind)
		},
	}
	cmd.Flags().StringVar(&site, "site", "", "filter to a single site by short name")
	cmd.Flags().StringVar(&branch, "branch", "", "filter to a single worktree branch")
	cmd.Flags().StringVar(&ctxKind, "ctx", "", "filter by context type: fpm or cli")
	return cmd
}

func runDumpToggle(enable bool) error {
	res, err := dumpsops.Apply(enable)
	if err != nil {
		return err
	}
	state := "disabled"
	if res.Enabled {
		state = "enabled"
	}
	feedback.Begin()
	if res.NoChange {
		feedback.Line("debug bridge already " + state)
		return nil
	}
	if res.Enabled {
		feedback.Done("debug bridge enabled")
		feedback.Note("next dump() / dd() call lands in the dashboard")
	} else {
		feedback.Done("debug bridge disabled")
	}
	nudgeUIDumpsChanged()
	return nil
}

// nudgeUIDumpsChanged is a best-effort ping to lerd-ui so every connected
// dashboard tab refreshes its dump-bridge indicator over the WebSocket
// instead of waiting for the next manual reload. Silent on any error: a
// missing lerd-ui means there are no WS subscribers to update anyway.
func nudgeUIDumpsChanged() {
	_, _, _ = postUnix("/api/dumps/notify-changed", nil)
}

func runDumpStatus(_ *cobra.Command, _ []string) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	state := feedback.Amber("disabled")
	if cfg.IsDumpsEnabled() {
		state = feedback.Green("enabled")
	}
	fmt.Printf("Debug bridge: %s\n", state)
	fmt.Printf("Listener:    unix:%s\n", config.DumpsSocketPath())
	fmt.Printf("Bridge file: %s\n", config.DumpsBridgeFile())
	fmt.Printf("Bridge ini:  %s\n", config.DumpsIniFile())

	if !cfg.IsDumpsEnabled() {
		return nil
	}
	// Best-effort: ask lerd-ui for the buffer size. If the daemon isn't
	// running we just say so without erroring — `lerd dump status` should
	// be informational and never fail loudly.
	st, err := fetchStatus()
	if err != nil {
		fmt.Printf("Buffered:    (lerd-ui not reachable: %v)\n", err)
		return nil
	}
	fmt.Printf("Buffered:    %d event(s)\n", st.Count)
	if st.LastTS != "" {
		fmt.Printf("Last event:  %s\n", st.LastTS)
	}
	return nil
}

func runDumpClear() error {
	body, code, err := postUnix("/api/dumps/clear", nil)
	if err != nil {
		return fmt.Errorf("lerd-ui not reachable: %w", err)
	}
	if code != http.StatusOK && code != http.StatusNoContent {
		return fmt.Errorf("lerd-ui returned %d: %s", code, strings.TrimSpace(string(body)))
	}
	fmt.Println("Dump ring cleared.")
	return nil
}

func runDumpTail(site, branch, ctxKind string) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	if !cfg.IsDumpsEnabled() {
		fmt.Fprintln(os.Stderr, "[INFO] debug bridge is disabled. Run `lerd dump on` to enable.")
	}

	q := []string{}
	if site != "" {
		q = append(q, "site="+site)
	}
	if branch != "" {
		q = append(q, "branch="+branch)
	}
	if ctxKind != "" {
		if ctxKind != "fpm" && ctxKind != "cli" {
			return fmt.Errorf("--ctx must be fpm or cli, got %q", ctxKind)
		}
		q = append(q, "ctx="+ctxKind)
	}
	path := "/api/dumps/stream"
	if len(q) > 0 {
		path += "?" + strings.Join(q, "&")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	conn, err := dialUnixHTTP(ctx)
	if err != nil {
		return fmt.Errorf("lerd-ui not reachable on %s: %w", config.UIClientAddr(), err)
	}
	defer conn.Close()

	req, _ := http.NewRequestWithContext(ctx, "GET", "http://lerd"+path, nil)
	req.Header.Set("Accept", "text/event-stream")
	if err := req.Write(conn); err != nil {
		return err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("lerd-ui /api/dumps/stream returned %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), dumps.MaxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var ev dumps.Event
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			continue
		}
		printEvent(ev)
	}
	if err := scanner.Err(); err != nil && !isClosed(err) {
		return err
	}
	return nil
}

func printEvent(ev dumps.Event) {
	hdr := fmt.Sprintf("\033[36m[%s]\033[0m \033[35m%s\033[0m", ev.TS, ev.Ctx.Type)
	if ev.Ctx.Site != "" {
		hdr += " " + ev.Ctx.Site
		if ev.Ctx.Branch != "" {
			hdr += "@" + ev.Ctx.Branch
		}
	}
	if ev.Ctx.Request != "" {
		hdr += " " + ev.Ctx.Request
	}
	if ev.Src.File != "" {
		hdr += fmt.Sprintf(" \033[90m%s:%d\033[0m", ev.Src.File, ev.Src.Line)
	}
	fmt.Println(hdr)
	if ev.Label != "" {
		fmt.Printf("  \033[33m%s\033[0m\n", ev.Label)
	}
	if ev.Text != "" {
		// Indent the dumper text by two spaces for readability.
		for _, line := range strings.Split(strings.TrimRight(ev.Text, "\n"), "\n") {
			fmt.Println("  " + line)
		}
	}
	fmt.Println()
}

// statusResponse mirrors the JSON shape returned by GET /api/dumps?status=1.
type statusResponse struct {
	Enabled     bool   `json:"enabled"`
	Listening   bool   `json:"listening"`
	Addr        string `json:"addr"`
	Count       int    `json:"count"`
	Subscribers int    `json:"subscribers"`
	LastTS      string `json:"last_ts"`
}

func fetchStatus() (*statusResponse, error) {
	body, code, err := getUnix("/api/dumps/status")
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("status returned %d", code)
	}
	var st statusResponse
	if err := json.Unmarshal(body, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// uiClientDial reports the transport the CLI uses to reach the lerd-ui daemon:
// the unix socket on Linux, the TCP loopback on macOS (where the socket isn't
// created). It's a var so tests can point it at a fake listener regardless of
// the production per-OS default.
var uiClientDial = func() (network, addr string) {
	return config.UIClientNetwork(), config.UIClientAddr()
}

// dialUnixHTTP opens a TCP-style net.Conn to the lerd-ui daemon over whichever
// transport uiClientDial resolves (unix socket on Linux, TCP loopback on
// macOS). Used for the SSE tail; we keep a raw connection so we can stream the
// response body without a transport layer that buffers internally.
func dialUnixHTTP(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	network, addr := uiClientDial()
	return d.DialContext(ctx, network, addr)
}

func unixHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				network, addr := uiClientDial()
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, addr)
			},
		},
	}
}

func getUnix(path string) ([]byte, int, error) {
	req, _ := http.NewRequest("GET", "http://lerd"+path, nil)
	resp, err := unixHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

func postUnix(path string, body []byte) ([]byte, int, error) {
	req, _ := http.NewRequest("POST", "http://lerd"+path, strings.NewReader(string(body)))
	resp, err := unixHTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	return got, resp.StatusCode, err
}

func isClosed(err error) bool {
	if err == nil {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed") || strings.Contains(msg, "EOF")
}
