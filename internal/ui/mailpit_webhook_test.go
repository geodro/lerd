package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// broadcastCapture buffers wsMessages broadcast during a test. Reads and
// writes happen on different goroutines (the broker drains on its own),
// so all access is serialized via mu, without which go test -race trips
// on every assertion.
type broadcastCapture struct {
	mu   sync.Mutex
	msgs []wsMessage
}

func (b *broadcastCapture) append(m wsMessage) {
	b.mu.Lock()
	b.msgs = append(b.msgs, m)
	b.mu.Unlock()
}

func (b *broadcastCapture) len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.msgs)
}

func (b *broadcastCapture) snapshot() []wsMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]wsMessage, len(b.msgs))
	copy(out, b.msgs)
	return out
}

// captureBroadcast swaps the package-level broker for one whose peers we
// can drain. Returns a buffer that accumulates every wsMessage broadcast
// during the test and a cleanup func to restore the original broker.
func captureBroadcast(t *testing.T) (*broadcastCapture, func()) {
	t.Helper()
	prev := broker
	repl := &wsBroker{peers: make(map[chan wsMessage]struct{})}
	ch := repl.add()
	cap := &broadcastCapture{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range ch {
			cap.append(msg)
		}
	}()
	broker = repl
	return cap, func() {
		repl.remove(ch)
		<-done
		broker = prev
	}
}

func waitForBroadcast(cap *broadcastCapture) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && cap.len() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
}

// decodeMailNotification reads the generic push.Notification payload the
// mailpit webhook puts on the wire and extracts the mail-specific params
// (id, from, subject) that other tests assert on.
func decodeMailNotification(t *testing.T, raw []byte) (kind, titleKey, bodyKey, tag, urlPath string, params map[string]string, data map[string]string) {
	t.Helper()
	var n struct {
		Kind     string            `json:"kind"`
		Title    string            `json:"title"`
		TitleKey string            `json:"title_key"`
		Body     string            `json:"body"`
		BodyKey  string            `json:"body_key"`
		Tag      string            `json:"tag"`
		URL      string            `json:"url"`
		Params   map[string]string `json:"params"`
		Data     map[string]string `json:"data"`
	}
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("decode notification: %v; raw=%s", err, string(raw))
	}
	return n.Kind, n.TitleKey, n.BodyKey, n.Tag, n.URL, n.Params, n.Data
}

func TestMailpitWebhook_BroadcastsAsGenericNotification(t *testing.T) {
	got, cleanup := captureBroadcast(t)
	defer cleanup()

	payload := `{
		"ID": "abc123",
		"Subject": "Welcome!",
		"From": {"Name": "Alice", "Address": "alice@example.com"},
		"To": [{"Address": "user@astrolov.test"}],
		"Created": "2026-05-15T10:00:00Z"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/mailpit", strings.NewReader(payload))
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	handleMailpitWebhook(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}

	waitForBroadcast(got)
	if got.len() != 1 {
		t.Fatalf("broadcasts = %d, want 1", got.len())
	}
	msg := got.snapshot()[0]
	if len(msg.Kinds) != 1 || msg.Kinds[0] != "notification" {
		t.Errorf("Kinds = %v, want [notification]", msg.Kinds)
	}
	if msg.Notification == nil {
		t.Fatalf("Notification payload is nil")
	}
	kind, titleKey, bodyKey, tag, url, params, data := decodeMailNotification(t, msg.Notification)
	if kind != "mail" {
		t.Errorf("kind = %q, want mail", kind)
	}
	if titleKey != "notify_mail_title" {
		t.Errorf("title_key = %q, want notify_mail_title", titleKey)
	}
	if bodyKey != "notify_mail_body" {
		t.Errorf("body_key = %q, want notify_mail_body", bodyKey)
	}
	if params["subject"] != "Welcome!" {
		t.Errorf("params.subject = %q", params["subject"])
	}
	if params["from"] != "Alice <alice@example.com>" {
		t.Errorf("params.from = %q", params["from"])
	}
	if tag != "lerd-mail-abc123" {
		t.Errorf("tag = %q", tag)
	}
	if url != "#service/mailpit/view/abc123" {
		t.Errorf("url = %q", url)
	}
	if data["id"] != "abc123" {
		t.Errorf("data.id = %q", data["id"])
	}
}

func TestMailpitWebhook_AddressOnlySender(t *testing.T) {
	got, cleanup := captureBroadcast(t)
	defer cleanup()

	payload := `{"ID":"x","Subject":"","From":{"Address":"bob@example.com"},"Created":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/mailpit", strings.NewReader(payload))
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	handleMailpitWebhook(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	waitForBroadcast(got)
	if got.len() != 1 {
		t.Fatalf("broadcasts = %d, want 1", got.len())
	}
	_, _, _, _, _, params, _ := decodeMailNotification(t, got.snapshot()[0].Notification)
	if params["from"] != "bob@example.com" {
		t.Errorf("from = %q, want bob@example.com", params["from"])
	}
}

func TestMailpitWebhook_RejectsGet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/webhooks/mailpit", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	handleMailpitWebhook(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestMailpitWebhook_RejectsBadJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/mailpit", strings.NewReader("{not json"))
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	handleMailpitWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestMailpitWebhook_RejectsMissingID(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/mailpit", strings.NewReader(`{"Subject":"x"}`))
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	handleMailpitWebhook(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAssembleSnapshot_IncludesNotificationField(t *testing.T) {
	n := []byte(`{"kind":"mail","title":"x"}`)
	frame := assembleSnapshot(nil, nil, nil, nil, nil, n, []string{"notification"})
	var decoded struct {
		Type         string          `json:"type"`
		Notification json.RawMessage `json:"notification"`
	}
	if err := json.Unmarshal(frame, &decoded); err != nil {
		t.Fatalf("decode frame: %v; raw=%s", err, string(frame))
	}
	if decoded.Type != "notification" {
		t.Errorf("type = %q, want notification", decoded.Type)
	}
	if string(decoded.Notification) != string(n) {
		t.Errorf("notification payload = %s, want %s", string(decoded.Notification), string(n))
	}
}
