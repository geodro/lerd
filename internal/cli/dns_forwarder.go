package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// NewDNSForwarderCmd returns the hidden `lerd dns-forwarder` command that
// runs the userspace UDP+TCP relay used by `lerd lan:expose` to bridge LAN
// traffic to the rootless lerd-dns container. lan:expose internally needs
// this to make .test resolution work for remote machines.
//
// Why this exists: rootless podman + pasta cannot accept inbound packets on
// the host's LAN interface — pasta only intercepts loopback traffic via the
// host's /proc/net tables, and binding to a LAN-facing socket requires
// CAP_NET_RAW which rootless containers don't have. Without a userspace
// helper, `lan:expose` would only work for clients on the server itself.
//
// The forwarder runs as a systemd user service alongside lerd-watcher and
// lerd-ui, listening on <lan-ip>:5300 (UDP+TCP) and relaying every packet
// to 127.0.0.1:5300 where pasta does intercept correctly. The lerd binary
// owns this subcommand so users don't need socat or any other system tool.
func NewDNSForwarderCmd() *cobra.Command {
	var listen, forward string
	cmd := &cobra.Command{
		Use:    "dns-forwarder",
		Short:  "Userspace UDP+TCP relay used by `lerd lan:expose` (internal)",
		Hidden: true,
		Long: `Long-running daemon that relays UDP and TCP packets from --listen to
--forward. Used by lerd lan:expose to bridge LAN traffic to the rootless
lerd-dns container, since rootless pasta cannot bind on the host's LAN
interface directly.

Not intended to be invoked manually — managed by the
lerd-dns-forwarder.service systemd user unit that lerd lan:expose writes.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if listen == "" || forward == "" {
				return fmt.Errorf("--listen and --forward are required")
			}
			return runDNSForwarder(listen, forward)
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "", "<host:port> the forwarder listens on (e.g. 192.168.0.200:5300)")
	cmd.Flags().StringVar(&forward, "forward", "", "<host:port> packets are relayed to (e.g. 127.0.0.1:5300)")
	return cmd
}

// runDNSForwarder spawns the UDP and TCP relay goroutines and blocks on
// SIGTERM/SIGINT. Errors from either listener are fatal — systemd will
// restart the service automatically (Restart=on-failure in the unit).
func runDNSForwarder(listen, forward string) error {
	udpErr := make(chan error, 1)
	tcpErr := make(chan error, 1)

	go func() { udpErr <- runUDPRelay(listen, forward) }()
	go func() { tcpErr <- runTCPRelay(listen, forward) }()

	fmt.Fprintf(os.Stderr, "lerd dns-forwarder: relaying %s ↔ %s (UDP+TCP)\n", listen, forward)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-udpErr:
		return fmt.Errorf("udp relay: %w", err)
	case err := <-tcpErr:
		return fmt.Errorf("tcp relay: %w", err)
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "lerd dns-forwarder: caught %s, exiting\n", sig)
		return nil
	}
}

// runUDPRelay listens on listenAddr and forwards every datagram to
// forwardAddr, holding a per-client outbound socket so responses route back
// to the original sender. Sessions expire after 60 seconds of inactivity.
func runUDPRelay(listenAddr, forwardAddr string) error {
	laddr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("resolving listen %q: %w", listenAddr, err)
	}
	faddr, err := net.ResolveUDPAddr("udp", forwardAddr)
	if err != nil {
		return fmt.Errorf("resolving forward %q: %w", forwardAddr, err)
	}

	listener, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", listenAddr, err)
	}
	defer listener.Close()

	type session struct {
		out      *net.UDPConn
		lastUsed time.Time
	}
	var (
		mu       sync.Mutex
		sessions = map[string]*session{}
	)

	// Janitor: drop sessions idle for >60s.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			now := time.Now()
			for k, s := range sessions {
				if now.Sub(s.lastUsed) > 60*time.Second {
					_ = s.out.Close()
					delete(sessions, k)
				}
			}
			mu.Unlock()
		}
	}()

	buf := make([]byte, 65535)
	for {
		n, clientAddr, err := listener.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("read from %s: %w", listenAddr, err)
		}

		key := clientAddr.String()
		mu.Lock()
		sess, ok := sessions[key]
		if !ok {
			outConn, dialErr := net.DialUDP("udp", nil, faddr)
			if dialErr != nil {
				mu.Unlock()
				continue
			}
			sess = &session{out: outConn, lastUsed: time.Now()}
			sessions[key] = sess

			// Per-client response reader. Closes when the outbound conn closes.
			go func(s *session, addr *net.UDPAddr) {
				respBuf := make([]byte, 65535)
				for {
					_ = s.out.SetReadDeadline(time.Now().Add(60 * time.Second))
					n, err := s.out.Read(respBuf)
					if err != nil {
						mu.Lock()
						delete(sessions, addr.String())
						mu.Unlock()
						_ = s.out.Close()
						return
					}
					if _, werr := listener.WriteToUDP(respBuf[:n], addr); werr != nil {
						return
					}
				}
			}(sess, clientAddr)
		}
		sess.lastUsed = time.Now()
		mu.Unlock()

		if _, werr := sess.out.Write(buf[:n]); werr != nil {
			continue
		}
	}
}

// runTCPRelay listens on listenAddr and forwards every accepted connection
// to forwardAddr, copying bytes in both directions until either side closes.
func runTCPRelay(listenAddr, forwardAddr string) error {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", listenAddr, err)
	}
	defer listener.Close()

	for {
		client, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		go relayTCPConn(client, forwardAddr)
	}
}

func relayTCPConn(client net.Conn, forwardAddr string) {
	defer client.Close()
	target, err := net.Dial("tcp", forwardAddr)
	if err != nil {
		return
	}
	defer target.Close()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(target, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, target); done <- struct{}{} }()
	<-done
}
