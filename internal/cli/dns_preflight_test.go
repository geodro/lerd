package cli

import (
	"errors"
	"net"
	"os"
	"strings"
	"testing"
)

// forwarderRemoveRecorder records the teardown calls ensureLANForwarderRemoved
// makes and lets a test inject a RemoveServiceUnit error.
type forwarderRemoveRecorder struct {
	fakeServiceMgr
	calls     []string
	removeErr error
}

func (r *forwarderRemoveRecorder) Stop(name string) error {
	r.calls = append(r.calls, "stop:"+name)
	return nil
}
func (r *forwarderRemoveRecorder) Disable(name string) error {
	r.calls = append(r.calls, "disable:"+name)
	return nil
}
func (r *forwarderRemoveRecorder) RemoveServiceUnit(name string) error {
	r.calls = append(r.calls, "remove:"+name)
	return r.removeErr
}
func (r *forwarderRemoveRecorder) DaemonReload() error {
	r.calls = append(r.calls, "reload")
	return nil
}

func TestPreflightForwarderPort_OwnUnitActiveSkipsCheck(t *testing.T) {
	prevStatus := forwarderUnitStatusFn
	prevFree := forwarderPortFreeFn
	prevHolder := forwarderPortHolderFn
	t.Cleanup(func() {
		forwarderUnitStatusFn = prevStatus
		forwarderPortFreeFn = prevFree
		forwarderPortHolderFn = prevHolder
	})

	forwarderUnitStatusFn = func() string { return "active" }
	forwarderPortFreeFn = func(string, int) bool {
		t.Error("forwarderPortFreeFn must not be called when our forwarder is active")
		return true
	}
	forwarderPortHolderFn = func(string, int) string {
		t.Error("forwarderPortHolderFn must not be called when our forwarder is active")
		return ""
	}

	var events []string
	emit := func(s string) { events = append(events, s) }

	if err := preflightForwarderPort("192.168.1.10", emit); err != nil {
		t.Errorf("preflight should pass when our forwarder is active, got %v", err)
	}
	if len(events) == 0 || !strings.Contains(events[0], "skipping port check") {
		t.Errorf("expected progress message to mention skipped port check, got %v", events)
	}
}

func TestPreflightForwarderPort_DeactivatingAlsoSkipsCheck(t *testing.T) {
	prevStatus := forwarderUnitStatusFn
	prevFree := forwarderPortFreeFn
	t.Cleanup(func() {
		forwarderUnitStatusFn = prevStatus
		forwarderPortFreeFn = prevFree
	})

	forwarderUnitStatusFn = func() string { return "deactivating" }
	forwarderPortFreeFn = func(string, int) bool {
		t.Error("port check must not run while our own forwarder is deactivating")
		return true
	}

	if err := preflightForwarderPort("192.168.1.10", nil); err != nil {
		t.Errorf("deactivating status should skip the port check, got %v", err)
	}
}

func TestPreflightForwarderPort_PortFreeReturnsNil(t *testing.T) {
	prevStatus := forwarderUnitStatusFn
	prevFree := forwarderPortFreeFn
	t.Cleanup(func() {
		forwarderUnitStatusFn = prevStatus
		forwarderPortFreeFn = prevFree
	})

	forwarderUnitStatusFn = func() string { return "inactive" }
	forwarderPortFreeFn = func(string, int) bool { return true }

	var events []string
	if err := preflightForwarderPort("192.168.1.10", func(s string) { events = append(events, s) }); err != nil {
		t.Errorf("preflight should pass when port is free, got %v", err)
	}
	if len(events) != 1 || !strings.Contains(events[0], "192.168.1.10:5300") {
		t.Errorf("expected one probe progress line mentioning the address, got %v", events)
	}
}

func TestPreflightForwarderPort_PortInUseSurfacesHolder(t *testing.T) {
	prevStatus := forwarderUnitStatusFn
	prevFree := forwarderPortFreeFn
	prevHolder := forwarderPortHolderFn
	t.Cleanup(func() {
		forwarderUnitStatusFn = prevStatus
		forwarderPortFreeFn = prevFree
		forwarderPortHolderFn = prevHolder
	})

	forwarderUnitStatusFn = func() string { return "inactive" }
	forwarderPortFreeFn = func(string, int) bool { return false }
	forwarderPortHolderFn = func(host string, port int) string {
		return "  dnsmasq    1234 root  6u  IPv4  0x0  UDP " + host + ":5300"
	}

	err := preflightForwarderPort("192.168.1.10", nil)
	if err == nil {
		t.Fatal("expected preflight to fail when port is in use")
	}
	msg := err.Error()
	for _, want := range []string{"192.168.1.10:5300", "already in use", "dnsmasq", "lerd lan expose"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q\nfull message: %s", want, msg)
		}
	}
}

func TestEnsureLANForwarder_DarwinSkipsForwarderInstall(t *testing.T) {
	// macOS model: lerd-dns binds the LAN address directly, so installing the
	// forwarder would double-bind lanIP:5300 and crash lerd-dns. It must be
	// skipped entirely.
	prevBinds := lerdDNSBindsLANPort
	prevInstall := installLANForwarderFn
	t.Cleanup(func() {
		lerdDNSBindsLANPort = prevBinds
		installLANForwarderFn = prevInstall
	})

	lerdDNSBindsLANPort = true
	installLANForwarderFn = func(string, func(string)) error {
		t.Error("forwarder must not be installed when lerd-dns binds the LAN port directly")
		return nil
	}

	var events []string
	if err := ensureLANForwarder("192.168.1.10", func(s string) { events = append(events, s) }); err != nil {
		t.Errorf("ensureLANForwarder should be a no-op on the macOS model, got %v", err)
	}
	if len(events) == 0 || !strings.Contains(events[0], "no forwarder needed") {
		t.Errorf("expected a progress line explaining the skip, got %v", events)
	}
}

func TestEnsureLANForwarder_NonDarwinInstallsForwarder(t *testing.T) {
	// Linux model: lerd-dns can't bind the host LAN port, so the forwarder is
	// required and must be installed.
	prevBinds := lerdDNSBindsLANPort
	prevInstall := installLANForwarderFn
	t.Cleanup(func() {
		lerdDNSBindsLANPort = prevBinds
		installLANForwarderFn = prevInstall
	})

	lerdDNSBindsLANPort = false
	called := false
	installLANForwarderFn = func(lanIP string, _ func(string)) error {
		called = true
		if lanIP != "192.168.1.10" {
			t.Errorf("forwarder installed with wrong lanIP %q", lanIP)
		}
		return nil
	}

	if err := ensureLANForwarder("192.168.1.10", nil); err != nil {
		t.Errorf("ensureLANForwarder should install the forwarder on the Linux model, got %v", err)
	}
	if !called {
		t.Error("expected the forwarder to be installed on the Linux model")
	}
}

func TestEnsureLANForwarderRemoved_TearsDownUnit(t *testing.T) {
	// Teardown stops, disables, removes, then daemon-reloads, naming the
	// forwarder unit at each step, and emits a progress line.
	rec := &forwarderRemoveRecorder{}
	swapMgr(t, rec)

	var events []string
	if err := ensureLANForwarderRemoved(func(s string) { events = append(events, s) }); err != nil {
		t.Fatalf("ensureLANForwarderRemoved: unexpected error %v", err)
	}

	want := []string{
		"stop:lerd-dns-forwarder",
		"disable:lerd-dns-forwarder",
		"remove:lerd-dns-forwarder",
		"reload",
	}
	if !equalStrings(rec.calls, want) {
		t.Errorf("teardown calls: got %v want %v", rec.calls, want)
	}
	if len(events) == 0 || !strings.Contains(events[0], "lerd-dns-forwarder") {
		t.Errorf("expected a progress line naming the forwarder, got %v", events)
	}
}

func TestEnsureLANForwarderRemoved_MissingUnitIgnored(t *testing.T) {
	// A Mac that never had a forwarder (or any already-clean host) hits a
	// not-exist on removal; that is the no-op case and must not error.
	rec := &forwarderRemoveRecorder{removeErr: os.ErrNotExist}
	swapMgr(t, rec)

	if err := ensureLANForwarderRemoved(nil); err != nil {
		t.Errorf("a missing forwarder unit must be ignored, got %v", err)
	}
}

func TestEnsureLANForwarderRemoved_RealRemoveErrorSurfaces(t *testing.T) {
	// Any removal error that isn't not-exist is a genuine failure and must
	// abort the unexpose so the caller doesn't report a clean teardown.
	rec := &forwarderRemoveRecorder{removeErr: errors.New("permission denied")}
	swapMgr(t, rec)

	err := ensureLANForwarderRemoved(nil)
	if err == nil {
		t.Fatal("expected a real RemoveServiceUnit error to surface")
	}
	if !strings.Contains(err.Error(), "removing forwarder unit") {
		t.Errorf("error should wrap the forwarder removal, got %v", err)
	}
}

func TestForwarderPortFree_DetectsBoundUDP(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("could not bind a UDP port for the test: %v", err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port

	if forwarderPortFree("127.0.0.1", port) {
		t.Errorf("expected forwarderPortFree to report port %d as taken", port)
	}
}

func TestForwarderPortFree_DetectsBoundTCP(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("could not bind a TCP port for the test: %v", err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	if forwarderPortFree("127.0.0.1", port) {
		t.Errorf("expected forwarderPortFree to report TCP-bound port %d as taken", port)
	}
}

func TestForwarderPortFree_FreePortReturnsTrue(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("could not pick a free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	if !forwarderPortFree("127.0.0.1", port) {
		t.Errorf("expected just-released port %d to read as free", port)
	}
}

func TestForwarderPortFree_IPv6HostBracketedCorrectly(t *testing.T) {
	// net.JoinHostPort must bracket the IPv6 host; without it the
	// ListenPacket call would fail to parse the address and we'd
	// report "taken" for an actually-free port.
	if !forwarderPortFree("::1", 0) {
		// Port 0 → kernel-assigned, always free. If JoinHostPort wasn't
		// applied we'd get a parse error here.
		t.Errorf("expected IPv6 loopback with kernel-assigned port to read as free")
	}
}

func TestForwarderHolderFallbackHint(t *testing.T) {
	cases := []struct {
		name     string
		goos     string
		port     int
		wantSub  string
		wantPort string
	}{
		{"linux suggests ss", "linux", 5300, "ss -tulpn", ":5300"},
		{"darwin suggests lsof", "darwin", 5300, "lsof", ":5300"},
		{"other OS falls back to lsof", "freebsd", 42, "lsof", ":42"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := forwarderHolderFallbackHint(c.goos, c.port)
			if !strings.Contains(got, c.wantSub) {
				t.Errorf("hint for %q missing %q: %s", c.goos, c.wantSub, got)
			}
			if !strings.Contains(got, c.wantPort) {
				t.Errorf("hint for %q missing port substring %q: %s", c.goos, c.wantPort, got)
			}
		})
	}
}
