package dns

import (
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// LinkChanges opens a PF_ROUTE socket and emits a struct{} on out every time
// the kernel reports a host address or interface state change. It is the
// macOS counterpart of the Linux rtnetlink subscription: without it macOS
// would react to a network switch or DHCP renew only on the watcher's
// safety-net poll (up to 30s late), so a stale lan:expose .tld mapping and
// the dashboard DNS pill would linger red for up to a poll cycle. Message
// contents are discarded beyond the type filter, since callers only need the
// "something moved" signal to re-fingerprint DNS and re-probe.
//
// Returns nil after done closes, or a non-nil error if the route socket can't
// be opened so the caller can log a one-shot warning and fall back to its
// time-based poll.
func LinkChanges(out chan<- struct{}, done <-chan struct{}) error {
	// PF_ROUTE delivers all routing messages unsolicited; no bind is needed.
	// AF_UNSPEC as the protocol keeps both IPv4 and IPv6 address events.
	fd, err := unix.Socket(unix.AF_ROUTE, unix.SOCK_RAW, unix.AF_UNSPEC)
	if err != nil {
		return fmt.Errorf("route socket: %w", err)
	}
	defer unix.Close(fd)

	// macOS doesn't accept SOCK_NONBLOCK / SOCK_CLOEXEC in the socket type
	// (those are Linux extensions), so set both explicitly. Non-blocking lets
	// a done-close interrupt the read via Shutdown instead of leaking the
	// read goroutine on a blocked syscall.
	unix.CloseOnExec(fd)
	if err := unix.SetNonblock(fd, true); err != nil {
		return fmt.Errorf("route socket nonblock: %w", err)
	}

	go func() {
		<-done
		_ = unix.Shutdown(fd, unix.SHUT_RDWR)
	}()

	buf := make([]byte, 4096)
	for {
		n, err := unix.Read(fd, buf)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) {
				if waitErr := waitReadable(fd, done); waitErr != nil {
					return nil
				}
				continue
			}
			return nil
		}
		if n <= 0 {
			return nil
		}
		if routeBatchHasLinkChange(buf[:n]) {
			select {
			case out <- struct{}{}:
			default:
			}
		}
	}
}

// routeBatchHasLinkChange reports whether a single PF_ROUTE read (which can
// carry several concatenated messages) contains at least one address- or
// interface-level change. Every routing message starts with a host-byte-order
// rtm_msglen (u_short) followed by a version byte and a type byte, so the
// batch is walked by that length and the type inspected at offset 3.
//
// The type filter (RTM_NEWADDR / RTM_DELADDR / RTM_IFINFO / RTM_IFINFO2)
// mirrors the Linux subscription's RTMGRP_LINK | RTMGRP_IPV4_IFADDR |
// RTMGRP_IPV6_IFADDR group mask, so the watcher reacts to the same class of
// events (DHCP renew, network switch, VPN up/down) and ignores the unrelated
// unicast route churn that a PF_ROUTE socket also delivers.
func routeBatchHasLinkChange(b []byte) bool {
	for len(b) >= 4 {
		msglen := int(binary.NativeEndian.Uint16(b[0:2]))
		if msglen < 4 || msglen > len(b) {
			break
		}
		if isLinkChangeMsgType(b[3]) {
			return true
		}
		b = b[msglen:]
	}
	return false
}

// isLinkChangeMsgType reports whether a PF_ROUTE message type signals that the
// host's addressing or interface state shifted.
func isLinkChangeMsgType(t byte) bool {
	switch t {
	case unix.RTM_NEWADDR, unix.RTM_DELADDR, unix.RTM_IFINFO, unix.RTM_IFINFO2:
		return true
	default:
		return false
	}
}

// waitReadable blocks until fd has data or done closes. The route socket is
// non-blocking so a clean shutdown can interrupt the read by closing the
// socket, but we still need to park between bursts without spinning.
func waitReadable(fd int, done <-chan struct{}) error {
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		_, err := unix.Poll(pfd, 1000)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				select {
				case <-done:
					return errors.New("done")
				default:
					continue
				}
			}
			return err
		}
		select {
		case <-done:
			return errors.New("done")
		default:
		}
		if pfd[0].Revents&(unix.POLLIN|unix.POLLERR|unix.POLLHUP) != 0 {
			return nil
		}
	}
}
