package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/phpantom"
)

// handleLSPPhp bridges a browser WebSocket to a phpantom_lsp process so Monaco
// gets real, project-aware PHP intelligence in the tinker editor. One server
// process is spawned per connection, rooted at the site (or worktree) path,
// and torn down when the socket closes.
//
// The browser side (vscode-ws-jsonrpc) puts one JSON-RPC message per text
// frame; phpantom_lsp speaks the LSP stdio dialect with Content-Length headers.
// This handler is the framing translator between the two.
func handleLSPPhp(w http.ResponseWriter, r *http.Request) {
	ws, err := wsUpgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer ws.Close()

	// All frame writes funnel through here: the stdout pump and the ping
	// ticker both write, and the hand-rolled wsConn is not write-safe.
	var writeMu sync.Mutex
	sendText := func(b []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return ws.WriteText(b)
	}

	site, err := config.FindSiteByDomain(r.URL.Query().Get("domain"))
	if err != nil {
		return
	}
	root := resolveSitePath(site, r.URL.Query().Get("branch"))
	if root == "" {
		return
	}
	ensureWorktreeEnvIfBranch(site, r.URL.Query().Get("branch"))

	// First connect on a fresh install downloads the binary (a no-op after),
	// tied to the request context so closing the tab aborts the fetch. On
	// failure we just close: the browser falls back to a plain editor.
	if err := phpantom.EnsureBinary(r.Context(), io.Discard); err != nil {
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	cmd := exec.CommandContext(ctx, phpantom.BinPath())
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	// Hand the browser the resolved workspace root before any LSP traffic: it
	// needs the absolute host path to build the document URI and rootUri. This
	// is the only non-LSP frame on the wire, and always arrives first.
	if err := sendText([]byte(`{"type":"lerd-root","root":` + strconv.Quote(root) + `}`)); err != nil {
		return
	}

	// stdout (Content-Length framed) -> ws text frames.
	go func() {
		br := bufio.NewReader(stdout)
		for {
			msg, err := readLSPMessage(br)
			if err != nil {
				cancel()
				return
			}
			if err := sendText(msg); err != nil {
				cancel()
				return
			}
		}
	}()

	// Probe a silent socket so a dead browser tab releases the process.
	// Browsers auto-reply to pings; the pong refreshes the read deadline.
	go func() {
		t := time.NewTicker(wsPingInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				writeMu.Lock()
				err := ws.WritePing(nil)
				writeMu.Unlock()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	// ws frames -> stdin (Content-Length framed).
	for {
		if err := ws.SetReadDeadline(time.Now().Add(wsReadTimeout)); err != nil {
			return
		}
		op, payload, err := ws.ReadFrame()
		if err != nil {
			return
		}
		switch op {
		case wsOpText:
			if _, err := stdin.Write(encodeLSPMessage(payload)); err != nil {
				return
			}
		case wsOpPing:
			writeMu.Lock()
			_ = ws.WritePong(payload)
			writeMu.Unlock()
		case wsOpClose:
			writeMu.Lock()
			_ = ws.WriteClose()
			writeMu.Unlock()
			return
		}
	}
}

// maxLSPMessageBytes caps a single inbound LSP message. 64 MiB is far above
// any real completion/diagnostics payload but guards against a bad header.
const maxLSPMessageBytes = 64 << 20

// readLSPMessage reads one Content-Length framed JSON-RPC message body from an
// LSP stdio stream and returns just the JSON payload.
func readLSPMessage(br *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break // blank line terminates the header block
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
			if err != nil {
				return nil, fmt.Errorf("lsp: bad Content-Length %q", line)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("lsp: message without Content-Length")
	}
	// Bound the allocation so a malformed or runaway header can't make us
	// reserve gigabytes; real LSP messages are far below this.
	if contentLength > maxLSPMessageBytes {
		return nil, fmt.Errorf("lsp: message too large (%d bytes)", contentLength)
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, err
	}
	return body, nil
}

// encodeLSPMessage wraps a raw JSON-RPC payload in the Content-Length framing
// phpantom_lsp expects on stdin.
func encodeLSPMessage(body []byte) []byte {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	out := make([]byte, 0, len(header)+len(body))
	out = append(out, header...)
	out = append(out, body...)
	return out
}
