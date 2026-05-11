package dumps

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// removeStaleUnixSocket clears a Unix socket left behind by a prior process
// that exited without removing it. Real files (a developer accidentally
// wrote text there) are preserved; we only delete sockets.
func removeStaleUnixSocket(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket file %s", path)
	}
	return os.Remove(path)
}

// chmodUnixSocket loosens the socket's mode so the FPM containers (running
// as their own user inside podman) can connect through the %h:%h bind
// mount. 0660 matches lerd-ui's main socket.
func chmodUnixSocket(path string) error {
	return os.Chmod(path, 0660)
}

// DefaultAddr is the loopback bind address the receiver listens on when
// callers ask for tcp. Picked to avoid clashing with Symfony's
// `var-dump-server` default (9912) so users who already run it for some
// other purpose don't see surprise interceptions.
const DefaultAddr = "127.0.0.1:9913"

// DefaultNetwork is the listen network used by lerd-ui. Unix sockets are
// the default because the FPM containers can already reach the host home
// directory via the %h:%h bind mount — using a TCP loopback listener
// would mean opening the receiver to the entire host network ring just so
// containers could route through host.containers.internal.
const DefaultNetwork = "unix"

const (
	// MaxLineBytes caps a single dump payload before bufio drops the line.
	// Sized to absorb VarCloner output for moderately large objects without
	// letting a runaway dump pin memory unboundedly.
	MaxLineBytes = 4 << 20 // 4 MiB

	initialLineBuf = 64 << 10 // 64 KiB
)

// Server accepts loopback NDJSON connections from the PHP dump bridge,
// validates each line against ProtocolVersion, appends valid events to the
// ring, and publishes them to subscribers. Close once via Close().
type Server struct {
	addr   string
	ring   *Ring
	hub    *Hub
	ln     net.Listener
	wg     sync.WaitGroup
	closed chan struct{}
	once   sync.Once
}

// Listen binds a TCP listener on addr and starts the accept loop. If addr
// is empty, DefaultAddr is used. The returned Server keeps running until
// Close is called or ctx is cancelled. Kept for tests and existing callers
// that already pass a TCP address; production lerd-ui uses ListenOn with
// "unix" so the receiver isn't reachable from the host network ring.
func Listen(ctx context.Context, addr string) (*Server, error) {
	if addr == "" {
		addr = DefaultAddr
	}
	return ListenOn(ctx, "tcp", addr)
}

// ListenOn binds on the given network and address. network is either "tcp"
// or "unix"; for "unix", any pre-existing socket at addr is removed first
// so a previous lerd-ui crash doesn't pin the path forever.
func ListenOn(ctx context.Context, network, addr string) (*Server, error) {
	if network == "" {
		network = DefaultNetwork
	}
	if addr == "" {
		if network == "tcp" {
			addr = DefaultAddr
		}
	}
	if network == "unix" {
		_ = removeStaleUnixSocket(addr)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("dumps: listen %s %s: %w", network, addr, err)
	}
	if network == "unix" {
		if err := chmodUnixSocket(addr); err != nil {
			_ = ln.Close()
			return nil, err
		}
	}
	s := &Server{
		addr:   ln.Addr().String(),
		ring:   NewRing(0),
		hub:    NewHub(),
		ln:     ln,
		closed: make(chan struct{}),
	}
	s.wg.Add(1)
	go s.acceptLoop()
	if ctx != nil && ctx.Done() != nil {
		// context.Background()'s Done() is nil, so we'd otherwise leak a
		// goroutine blocked on `<-nil` for the lifetime of the test/process.
		go func() {
			<-ctx.Done()
			_ = s.Close()
		}()
	}
	return s, nil
}

// Addr returns the bound address, useful when Listen was called with :0.
func (s *Server) Addr() string { return s.addr }

// Snapshot returns a copy of the ring in insertion order.
func (s *Server) Snapshot() []Event { return s.ring.Snapshot() }

// Filter returns a filtered Snapshot.
func (s *Server) Filter(opts FilterOpts) []Event { return s.ring.Filter(opts) }

// Subscribe returns a buffered channel of new events plus an unsubscribe func.
func (s *Server) Subscribe() (<-chan Event, func()) { return s.hub.Subscribe() }

// Clear empties the ring. Active subscribers continue to receive events.
func (s *Server) Clear() { s.ring.Clear() }

// Len returns the number of buffered events.
func (s *Server) Len() int { return s.ring.Len() }

// Subscribers returns the current subscriber count.
func (s *Server) Subscribers() int { return s.hub.Count() }

// Push injects an event as if it had arrived on the wire. Used by tests to
// avoid juggling sockets when only ring/hub semantics matter.
func (s *Server) Push(e Event) {
	if !e.Valid() {
		return
	}
	s.ring.Append(e)
	s.hub.Publish(e)
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// Transient accept error — back off briefly and try again.
			time.Sleep(50 * time.Millisecond)
			continue
		}
		s.wg.Add(1)
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	// Bound the read so a stuck bridge can't tie up a goroutine forever.
	// The bridge writes one event per connection and disconnects.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, initialLineBuf), MaxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if !ev.Valid() {
			continue
		}
		s.ring.Append(ev)
		s.hub.Publish(ev)
	}
	// scanner.Err() is intentionally swallowed: the bridge is fire-and-forget
	// and connection-level failures (timeout, oversized line) must not affect
	// the user's request. Logging them would just spam.
}

// Close stops accepting connections, closes in-flight reads, and waits for
// outstanding goroutines to drain. Safe to call multiple times.
func (s *Server) Close() error {
	s.once.Do(func() {
		close(s.closed)
		if s.ln != nil {
			_ = s.ln.Close()
		}
	})
	s.wg.Wait()
	return nil
}
