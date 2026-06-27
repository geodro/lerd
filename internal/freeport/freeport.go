// Package freeport provides a shared, dependency-free TCP host-port allocator:
// a dual-stack bindability probe and a predicate-injected free-port search. It
// is a leaf package (stdlib only) so both internal/cli (host-proxy dev servers)
// and internal/serviceops (the service port-ownership guard) can import it
// without an import cycle, and so the search logic stays unit-testable without
// binding real sockets.
package freeport

import (
	"net"
	"strconv"
)

// Bindable reports whether a TCP port can be bound on both loopback stacks
// (127.0.0.1 and [::1]) — the two addresses lerd's published quadlets and
// host-proxy dev servers bind. A bind test is stricter and more accurate than a
// dial test for "can we publish here": it catches a port reserved on either
// stack, not just one with a live listener. A host with no IPv6 loopback at all
// is tolerated — the v6 check is skipped rather than treated as busy. The v4
// listener is held open (deferred close) through the v6 bind so the pair is
// tested atomically — closing it early would let another process grab v4 in the
// window between the two checks.
func Bindable(port int) bool {
	ln4, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	defer ln4.Close()
	ln6, err := net.Listen("tcp", net.JoinHostPort("::1", strconv.Itoa(port)))
	if err != nil {
		// Distinguish "port already taken on ::1" from "this host has no IPv6 loopback".
		probe, perr := net.Listen("tcp", "[::1]:0")
		if perr != nil {
			return true // no IPv6 loopback here; the v4 bind is sufficient
		}
		_ = probe.Close()
		return false // IPv6 works but this port is taken on ::1
	}
	_ = ln6.Close()
	return true
}

// FirstFree returns the first port at or above start for which taken reports
// false. The predicate is injected so the search is unit-testable without
// binding real sockets; callers compose it from Bindable plus any reserved-port
// set of their own. start is clamped to >= 1. Returns 0 when nothing in
// [start, 65535] is free, so callers can decide their own fallback.
func FirstFree(start int, taken func(int) bool) int {
	if start < 1 {
		start = 1
	}
	for p := start; p <= 65535; p++ {
		if !taken(p) {
			return p
		}
	}
	return 0
}
