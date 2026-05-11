package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/dumps"
)

// withDumpsServer swaps the package-level receiver for a fresh in-memory
// instance for the duration of the test. The handler functions read the
// pointer at call time so each test gets a clean ring + hub.
func withDumpsServer(t *testing.T) *dumps.Server {
	t.Helper()
	srv, err := dumps.Listen(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("dumps.Listen: %v", err)
	}
	prev := dumpsServer.Load()
	dumpsServer.Store(srv)
	t.Cleanup(func() {
		_ = srv.Close()
		dumpsServer.Store(prev)
	})
	return srv
}

func TestHandleDumpsList_EmptyWhenNoServer(t *testing.T) {
	prev := dumpsServer.Load()
	dumpsServer.Store(nil)
	t.Cleanup(func() { dumpsServer.Store(prev) })

	req := httptest.NewRequest("GET", "/api/dumps", nil)
	rec := httptest.NewRecorder()
	handleDumpsList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got []dumps.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %v", got)
	}
}

func TestHandleDumpsList_ReturnsBuffered(t *testing.T) {
	srv := withDumpsServer(t)
	srv.Push(dumps.Event{V: 1, ID: "a", Kind: "dump", Ctx: dumps.Context{Type: "fpm", Site: "acme"}})
	srv.Push(dumps.Event{V: 1, ID: "b", Kind: "dump", Ctx: dumps.Context{Type: "cli", Site: "acme"}})

	req := httptest.NewRequest("GET", "/api/dumps", nil)
	rec := httptest.NewRecorder()
	handleDumpsList(rec, req)

	var got []dumps.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" {
		t.Errorf("got %v", got)
	}
}

func TestHandleDumpsList_FiltersBySite(t *testing.T) {
	srv := withDumpsServer(t)
	srv.Push(dumps.Event{V: 1, ID: "a", Kind: "dump", Ctx: dumps.Context{Type: "fpm", Site: "acme"}})
	srv.Push(dumps.Event{V: 1, ID: "b", Kind: "dump", Ctx: dumps.Context{Type: "fpm", Site: "other"}})

	req := httptest.NewRequest("GET", "/api/dumps?site=acme", nil)
	rec := httptest.NewRecorder()
	handleDumpsList(rec, req)

	var got []dumps.Event
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("got %v", got)
	}
}

func TestHandleDumpsList_FiltersByCtxAndSince(t *testing.T) {
	srv := withDumpsServer(t)
	for _, id := range []string{"a", "b", "c", "d"} {
		srv.Push(dumps.Event{V: 1, ID: id, Kind: "dump", Ctx: dumps.Context{Type: "fpm"}})
	}
	srv.Push(dumps.Event{V: 1, ID: "x-cli", Kind: "dump", Ctx: dumps.Context{Type: "cli"}})

	req := httptest.NewRequest("GET", "/api/dumps?ctx=fpm&since=b", nil)
	rec := httptest.NewRecorder()
	handleDumpsList(rec, req)
	var got []dumps.Event
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 2 || got[0].ID != "c" || got[1].ID != "d" {
		t.Errorf("got %v", got)
	}
}

func TestHandleDumpsClear_Loopback(t *testing.T) {
	srv := withDumpsServer(t)
	srv.Push(dumps.Event{V: 1, ID: "a", Kind: "dump"})
	if srv.Len() != 1 {
		t.Fatal("setup: ring should have 1")
	}
	req := httptest.NewRequest("POST", "/api/dumps/clear", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleDumpsClear(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if srv.Len() != 0 {
		t.Errorf("ring not cleared, len = %d", srv.Len())
	}
}

func TestHandleDumpsClear_RejectsNonLoopback(t *testing.T) {
	withDumpsServer(t)
	req := httptest.NewRequest("POST", "/api/dumps/clear", nil)
	req.RemoteAddr = "192.168.1.50:42000"
	rec := httptest.NewRecorder()
	handleDumpsClear(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestHandleDumpsStatus_NoServerStillReturnsConfig(t *testing.T) {
	prev := dumpsServer.Load()
	dumpsServer.Store(nil)
	t.Cleanup(func() { dumpsServer.Store(prev) })

	req := httptest.NewRequest("GET", "/api/dumps/status", nil)
	rec := httptest.NewRecorder()
	handleDumpsStatus(rec, req)

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["listening"] != false {
		t.Errorf("listening = %v, want false", got["listening"])
	}
	if got["addr"] == "" {
		t.Errorf("addr should default to DefaultAddr even with no server")
	}
}

// flusherRecorder wraps httptest.ResponseRecorder so handleDumpsStream sees
// http.Flusher. The recorder's body grows synchronously, which is exactly
// what we want so the test can assert on emitted SSE bytes deterministically.
type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (f *flusherRecorder) Flush() { f.flushes++ }

func TestHandleDumpsStream_ReplaysSnapshotThenExitsOnContextCancel(t *testing.T) {
	srv := withDumpsServer(t)
	srv.Push(dumps.Event{V: 1, ID: "old1", Kind: "dump"})
	srv.Push(dumps.Event{V: 1, ID: "old2", Kind: "dump"})

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/dumps/stream", nil).WithContext(ctx)
	rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		handleDumpsStream(rec, req)
	}()

	// Wait until the replay events are in the body, then cancel.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Count(rec.Body.Bytes(), []byte("data:")) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after context cancel")
	}

	body := rec.Body.String()
	for _, id := range []string{"old1", "old2"} {
		if !strings.Contains(body, id) {
			t.Errorf("SSE body missing %q\n--- body ---\n%s", id, body)
		}
	}
	if !strings.Contains(body, "text/event-stream") && rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", rec.Header().Get("Content-Type"))
	}
}

func TestHandleDumpsStream_DeliversLiveEvent(t *testing.T) {
	srv := withDumpsServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/dumps/stream", nil).WithContext(ctx)
	rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		handleDumpsStream(rec, req)
	}()

	// Give the handler a moment to subscribe.
	time.Sleep(50 * time.Millisecond)
	srv.Push(dumps.Event{V: 1, ID: "live1", Kind: "dump"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.Body.String(), "live1") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	if !strings.Contains(rec.Body.String(), "live1") {
		t.Errorf("live event missing\n--- body ---\n%s", rec.Body.String())
	}
}
