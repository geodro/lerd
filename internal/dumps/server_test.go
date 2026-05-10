package dumps

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

// dialAndSend writes payload to addr and closes the connection. Each call
// uses a fresh connection, mirroring the bridge's fire-and-forget pattern.
func dialAndSend(t *testing.T, addr, payload string) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func startServer(t *testing.T) *Server {
	t.Helper()
	s, err := Listen(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func TestServer_AcceptsValidEvent(t *testing.T) {
	s := startServer(t)

	dialAndSend(t, s.Addr(), mustJSON(t, Event{
		V: 1, ID: "a", TS: "2026-05-10T00:00:00.000Z", Kind: "dump",
		Ctx: Context{Type: "cli", Site: "x"},
	}))

	if !waitFor(func() bool { return s.Len() == 1 }, time.Second) {
		t.Fatalf("event never landed in ring; len=%d", s.Len())
	}
	got := s.Snapshot()
	if got[0].ID != "a" {
		t.Errorf("ring[0].ID = %q, want a", got[0].ID)
	}
}

func TestServer_DropsInvalidJSON(t *testing.T) {
	s := startServer(t)
	dialAndSend(t, s.Addr(), "not-json\n")
	dialAndSend(t, s.Addr(), `{"v":1,"id":"good","kind":"dump"}`+"\n")
	if !waitFor(func() bool { return s.Len() == 1 }, time.Second) {
		t.Fatalf("expected exactly 1 valid event, got %d", s.Len())
	}
	if s.Snapshot()[0].ID != "good" {
		t.Errorf("wrong event accepted: %q", s.Snapshot()[0].ID)
	}
}

func TestServer_DropsWrongProtocolVersion(t *testing.T) {
	s := startServer(t)
	dialAndSend(t, s.Addr(), `{"v":99,"id":"x","kind":"dump"}`+"\n")
	dialAndSend(t, s.Addr(), `{"v":1,"id":"y","kind":"dump"}`+"\n")
	if !waitFor(func() bool { return s.Len() == 1 }, time.Second) {
		t.Fatalf("expected 1 valid event, got %d", s.Len())
	}
}

func TestServer_DropsMissingFields(t *testing.T) {
	s := startServer(t)
	dialAndSend(t, s.Addr(), `{"v":1,"kind":"dump"}`+"\n")          // missing id
	dialAndSend(t, s.Addr(), `{"v":1,"id":"x"}`+"\n")               // missing kind
	dialAndSend(t, s.Addr(), `{"v":1,"id":"y","kind":"dump"}`+"\n") // good
	if !waitFor(func() bool { return s.Len() == 1 }, time.Second) {
		t.Fatalf("expected 1 valid event, got %d", s.Len())
	}
}

func TestServer_DropsOversizedLine(t *testing.T) {
	s := startServer(t)
	huge := strings.Repeat("x", MaxLineBytes+1024)
	payload := `{"v":1,"id":"big","kind":"dump","text":"` + huge + `"}` + "\n"
	dialAndSend(t, s.Addr(), payload)
	dialAndSend(t, s.Addr(), `{"v":1,"id":"ok","kind":"dump"}`+"\n")
	if !waitFor(func() bool { return s.Len() == 1 }, time.Second) {
		t.Fatalf("expected 1 valid event, got %d", s.Len())
	}
	if s.Snapshot()[0].ID != "ok" {
		t.Errorf("oversized line was accepted")
	}
}

func TestServer_HandlesMultipleEventsOnOneConnection(t *testing.T) {
	s := startServer(t)
	payload := `{"v":1,"id":"a","kind":"dump"}` + "\n" +
		`{"v":1,"id":"b","kind":"dump"}` + "\n" +
		`{"v":1,"id":"c","kind":"dump"}` + "\n"
	dialAndSend(t, s.Addr(), payload)
	if !waitFor(func() bool { return s.Len() == 3 }, time.Second) {
		t.Fatalf("expected 3 events, got %d", s.Len())
	}
}

func TestServer_PublishesToSubscribers(t *testing.T) {
	s := startServer(t)
	ch, unsub := s.Subscribe()
	defer unsub()

	dialAndSend(t, s.Addr(), `{"v":1,"id":"sub","kind":"dump"}`+"\n")
	select {
	case e := <-ch:
		if e.ID != "sub" {
			t.Errorf("got %q, want sub", e.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber timed out")
	}
}

func TestServer_PushBypassesSocket(t *testing.T) {
	s := startServer(t)
	s.Push(Event{V: 1, ID: "p", Kind: "dump"})
	if s.Len() != 1 {
		t.Errorf("Push len = %d, want 1", s.Len())
	}
	// Invalid event must be dropped.
	s.Push(Event{ID: "bad"})
	if s.Len() != 1 {
		t.Errorf("invalid Push appended; len = %d", s.Len())
	}
}

func TestServer_CloseIsIdempotent(t *testing.T) {
	s, err := Listen(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}

func waitFor(cond func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
