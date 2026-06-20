package dns

import (
	"encoding/binary"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// routeMsg builds a synthetic PF_ROUTE message of the given type and total
// byte length, with the host-byte-order rtm_msglen prefix the parser walks
// by. length is clamped to the 4-byte header minimum.
func routeMsg(msgType byte, length int) []byte {
	if length < 4 {
		length = 4
	}
	b := make([]byte, length)
	binary.NativeEndian.PutUint16(b[0:2], uint16(length))
	b[2] = 5 // RTM_VERSION
	b[3] = msgType
	return b
}

func TestRouteBatchHasLinkChange(t *testing.T) {
	cases := []struct {
		name string
		buf  []byte
		want bool
	}{
		{"empty", nil, false},
		{"short fragment ignored", []byte{0x01, 0x02}, false},
		{"new address", routeMsg(unix.RTM_NEWADDR, 28), true},
		{"del address", routeMsg(unix.RTM_DELADDR, 28), true},
		{"ifinfo", routeMsg(unix.RTM_IFINFO, 32), true},
		{"ifinfo2", routeMsg(unix.RTM_IFINFO2, 40), true},
		{"unrelated unicast route add", routeMsg(unix.RTM_ADD, 64), false},
		{"unrelated route get", routeMsg(unix.RTM_GET, 64), false},
		{
			name: "batch: unrelated then address change",
			buf:  append(routeMsg(unix.RTM_ADD, 48), routeMsg(unix.RTM_NEWADDR, 28)...),
			want: true,
		},
		{
			name: "batch: only unrelated",
			buf:  append(routeMsg(unix.RTM_ADD, 48), routeMsg(unix.RTM_DELETE, 48)...),
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := routeBatchHasLinkChange(tc.buf); got != tc.want {
				t.Fatalf("routeBatchHasLinkChange = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRouteBatchHasLinkChange_truncatedLengthStops guards the walk against a
// bogus msglen that overruns the buffer: parsing must stop rather than slice
// out of range, and a relevant message before the bad one still counts.
func TestRouteBatchHasLinkChange_truncatedLengthStops(t *testing.T) {
	good := routeMsg(unix.RTM_NEWADDR, 28)
	bad := make([]byte, 8)
	binary.NativeEndian.PutUint16(bad[0:2], 9999) // claims far more than present
	bad[3] = unix.RTM_ADD

	if !routeBatchHasLinkChange(append(append([]byte{}, good...), bad...)) {
		t.Fatal("relevant message before a truncated one should still report true")
	}
	if routeBatchHasLinkChange(bad) {
		t.Fatal("a lone truncated unrelated message should report false, not panic")
	}
}

// TestLinkChanges_lifecycle opens the real PF_ROUTE socket and asserts a clean
// shutdown returns nil. Darwin-only by filename, so it runs on the dev mac.
func TestLinkChanges_lifecycle(t *testing.T) {
	out := make(chan struct{}, 16)
	done := make(chan struct{})
	errCh := make(chan error, 1)
	go func() { errCh <- LinkChanges(out, done) }()

	// Let the read loop reach its blocked state before tearing down.
	time.Sleep(100 * time.Millisecond)
	close(done)

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("LinkChanges returned %v after clean shutdown, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("LinkChanges did not return after done close")
	}
}
