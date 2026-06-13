package ui

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/idle"
)

// TestReadAccessFeed_recordsTouch drives a real unix datagram socket with an
// nginx-style syslog access line and asserts it lands as a site touch. This is
// the part the tracker units can't cover: the actual socket read plus the
// syslog framing nginx wraps the "$host" message in.
func TestReadAccessFeed_recordsTouch(t *testing.T) {
	prev := activityTracker
	t.Cleanup(func() { activityTracker = prev })
	activityTracker = idle.NewTracker(func(h string) (string, bool) {
		if h == "myapp.test" {
			return "myapp", true
		}
		return "", false
	})

	sock := filepath.Join(t.TempDir(), "access.sock")
	conn, err := net.ListenPacket("unixgram", sock)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	go readAccessFeed(conn)

	sender, err := net.Dial("unixgram", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sender.Close()
	if _, err := sender.Write([]byte("<190>Jun 12 10:00:00 lerdaccess: myapp.test")); err != nil {
		t.Fatalf("send: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := activityTracker.LastActive("myapp"); ok {
			return // recorded as expected
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("access datagram was not recorded as a site touch within 2s")
}

// TestReadAccessFeed_ignoresUnmatched confirms a datagram for an unknown host
// never creates a phantom record.
func TestReadAccessFeed_ignoresUnmatched(t *testing.T) {
	prev := activityTracker
	t.Cleanup(func() { activityTracker = prev })
	activityTracker = idle.NewTracker(func(string) (string, bool) { return "", false })

	sock := filepath.Join(t.TempDir(), "access.sock")
	conn, err := net.ListenPacket("unixgram", sock)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	go readAccessFeed(conn)

	sender, err := net.Dial("unixgram", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sender.Close()
	sender.Write([]byte("stranger.test")) //nolint:errcheck

	time.Sleep(50 * time.Millisecond)
	if len(activityTracker.Snapshot()) != 0 {
		t.Error("unmatched host must not create a record")
	}
}
