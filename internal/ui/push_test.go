package ui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/geodro/lerd/internal/push"
)

// withFreshDataDir gives each ui-push test its own XDG_DATA_HOME so VAPID
// keys + subscription files don't bleed between tests.
func withFreshDataDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	prev, hadPrev := os.LookupEnv("XDG_DATA_HOME")
	t.Setenv("XDG_DATA_HOME", dir)
	t.Cleanup(func() {
		if hadPrev {
			os.Setenv("XDG_DATA_HOME", prev)
		} else {
			os.Unsetenv("XDG_DATA_HOME")
		}
	})
}

func TestHandlePushVAPIDPublicKey(t *testing.T) {
	withFreshDataDir(t)

	req := httptest.NewRequest(http.MethodGet, "/api/push/vapid-public-key", nil)
	rec := httptest.NewRecorder()
	handlePushVAPIDPublicKey(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PublicKey == "" {
		t.Errorf("public_key is empty")
	}
}

func TestHandlePushSubscribe_AcceptsAndStoresSubscription(t *testing.T) {
	withFreshDataDir(t)

	body := []byte(`{
		"endpoint": "https://updates.push.services.mozilla.com/wpush/v2/abc",
		"keys": {"p256dh": "BPubKey", "auth": "AuthSecret"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/push/subscribe", bytes.NewReader(body))
	req.Header.Set("User-Agent", "TestUA/1.0")
	rec := httptest.NewRecorder()
	handlePushSubscribe(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	subs, err := push.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("subs len = %d, want 1", len(subs))
	}
	if subs[0].Endpoint != "https://updates.push.services.mozilla.com/wpush/v2/abc" {
		t.Errorf("endpoint = %q", subs[0].Endpoint)
	}
	if subs[0].UA != "TestUA/1.0" {
		t.Errorf("UA = %q, want TestUA/1.0", subs[0].UA)
	}
}

func TestHandlePushSubscribe_RejectsIncomplete(t *testing.T) {
	withFreshDataDir(t)

	cases := [][]byte{
		[]byte(`{"keys":{"p256dh":"p","auth":"a"}}`),
		[]byte(`{"endpoint":"https://x","keys":{"auth":"a"}}`),
		[]byte(`{"endpoint":"https://x","keys":{"p256dh":"p"}}`),
		[]byte(`not json`),
	}
	for i, b := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/push/subscribe", bytes.NewReader(b))
		rec := httptest.NewRecorder()
		handlePushSubscribe(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("case %d: status = %d, want 400", i, rec.Code)
		}
	}
}

func TestHandlePushUnsubscribe(t *testing.T) {
	withFreshDataDir(t)

	if err := push.Add(push.Subscription{
		Endpoint: "https://example.org/x",
		P256dh:   "p",
		Auth:     "a",
	}); err != nil {
		t.Fatalf("seeding subscription: %v", err)
	}

	body := []byte(`{"endpoint": "https://example.org/x"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/push/unsubscribe", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handlePushUnsubscribe(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	subs, _ := push.List()
	if len(subs) != 0 {
		t.Errorf("subs after unsubscribe = %d, want 0", len(subs))
	}
}
