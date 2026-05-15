package push

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// roundTripper captures every request webpush-go makes so tests can verify
// what we sent without spinning up real HTTP. It replies 201 Created (the
// success code push services return for accepted payloads) by default; a
// per-host override map can simulate 410 (subscription expired) so the
// pruning path is exercisable.
type roundTripper struct {
	mu     sync.Mutex
	calls  []*http.Request
	status map[string]int // hostname → status code
}

func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.calls = append(rt.calls, req)
	code := 201
	if rt.status != nil {
		if c, ok := rt.status[req.URL.Host]; ok {
			code = c
		}
	}
	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// fakeSubscription generates a syntactically valid PushSubscription with
// real P-256 keys so webpush-go's encryption layer doesn't reject the input.
// The endpoint string is arbitrary — we point it at a captured-by-stub URL.
func fakeSubscription(t *testing.T, endpoint string) Subscription {
	t.Helper()
	// p256dh: a fresh P-256 ECDH public key (uncompressed, base64url no pad).
	curve := ecdh.P256()
	pkey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	pub := pkey.PublicKey().Bytes()
	// auth: 16 random bytes, base64url no pad.
	authBytes := make([]byte, 16)
	if _, err := rand.Read(authBytes); err != nil {
		t.Fatal(err)
	}
	return Subscription{
		Endpoint: endpoint,
		P256dh:   base64.RawURLEncoding.EncodeToString(pub),
		Auth:     base64.RawURLEncoding.EncodeToString(authBytes),
		Enabled:  true,
	}
}

// withStubTransport replaces HTTPClient with one whose RoundTripper records
// every outbound request. Returns the recorder + cleanup.
func withStubTransport(t *testing.T) *roundTripper {
	t.Helper()
	rt := &roundTripper{status: map[string]int{}}
	prev := HTTPClient
	HTTPClient = &http.Client{Transport: rt}
	t.Cleanup(func() { HTTPClient = prev })
	return rt
}

// freshECDSAKeys returns a fresh VAPID keypair (private base64url, public
// base64url-uncompressed) so the test doesn't depend on disk state.
func freshECDSAKeys(t *testing.T) (string, string) {
	t.Helper()
	pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	priv := base64.RawURLEncoding.EncodeToString(pk.D.Bytes())
	pub := base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(), pk.X, pk.Y))
	return priv, pub
}

func TestSend_EmptyStoreIsNoOp(t *testing.T) {
	withTempDataDir(t)
	rt := withStubTransport(t)

	if err := Send(Notification{Kind: "x", Title: "y"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(rt.calls) != 0 {
		t.Errorf("expected no HTTP calls with empty store, got %d", len(rt.calls))
	}
}

func TestSend_FansOutToEverySubscription(t *testing.T) {
	withTempDataDir(t)
	rt := withStubTransport(t)
	if err := Add(fakeSubscription(t, "https://push1.example.com/sub-a")); err != nil {
		t.Fatal(err)
	}
	if err := Add(fakeSubscription(t, "https://push2.example.com/sub-b")); err != nil {
		t.Fatal(err)
	}

	if err := Send(Notification{Kind: "test", Title: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(rt.calls) != 2 {
		t.Fatalf("expected 2 HTTP calls, got %d", len(rt.calls))
	}
	hosts := map[string]bool{}
	for _, c := range rt.calls {
		hosts[c.URL.Host] = true
	}
	if !hosts["push1.example.com"] || !hosts["push2.example.com"] {
		t.Errorf("missed a host: %v", hosts)
	}
}

func TestSend_ThreadsTTLAndUrgencyIntoHeaders(t *testing.T) {
	withTempDataDir(t)
	rt := withStubTransport(t)
	if err := Add(fakeSubscription(t, "https://push.example.com/sub")); err != nil {
		t.Fatal(err)
	}

	if err := Send(Notification{Kind: "op", Title: "done", TTL: 3600, Urgency: "low"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(rt.calls) != 1 {
		t.Fatalf("calls = %d", len(rt.calls))
	}
	req := rt.calls[0]
	if got := req.Header.Get("TTL"); got != "3600" {
		t.Errorf("TTL header = %q, want 3600", got)
	}
	if got := req.Header.Get("Urgency"); got != "low" {
		t.Errorf("Urgency header = %q, want low", got)
	}
}

func TestSend_DefaultsToNormalUrgencyWhenUnset(t *testing.T) {
	withTempDataDir(t)
	rt := withStubTransport(t)
	if err := Add(fakeSubscription(t, "https://push.example.com/sub")); err != nil {
		t.Fatal(err)
	}

	if err := Send(Notification{Kind: "mail", Title: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := rt.calls[0].Header.Get("Urgency"); got != "normal" {
		t.Errorf("Urgency = %q, want normal default", got)
	}
}

func TestSend_PrunesSubscriptionOn410(t *testing.T) {
	withTempDataDir(t)
	rt := withStubTransport(t)
	rt.status["dead.push.example.com"] = http.StatusGone
	dead := fakeSubscription(t, "https://dead.push.example.com/abc")
	live := fakeSubscription(t, "https://live.push.example.com/xyz")
	if err := Add(dead); err != nil {
		t.Fatal(err)
	}
	if err := Add(live); err != nil {
		t.Fatal(err)
	}

	if err := Send(Notification{Kind: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	subs, _ := List()
	if len(subs) != 1 {
		t.Fatalf("expected 1 remaining sub (live one), got %d", len(subs))
	}
	if subs[0].Endpoint != live.Endpoint {
		t.Errorf("wrong sub kept: %q", subs[0].Endpoint)
	}
}

// TestSend_PayloadShapeReachesTransport sanity-checks that the body the
// push service receives is a real RFC 8291 aes128gcm envelope (binary, with
// the Content-Encoding header). Verifying the precise ciphertext would
// duplicate webpush-go's tests; we just confirm the encryption layer ran
// and our Notification ended up as the source.
func TestSend_PayloadShapeReachesTransport(t *testing.T) {
	withTempDataDir(t)
	rt := withStubTransport(t)
	if err := Add(fakeSubscription(t, "https://push.example.com/sub")); err != nil {
		t.Fatal(err)
	}

	if err := Send(Notification{Kind: "mail", Title: "T", Body: "B"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	req := rt.calls[0]
	if got := req.Header.Get("Content-Encoding"); got != "aes128gcm" {
		t.Errorf("Content-Encoding = %q, want aes128gcm", got)
	}
	if req.Body == nil {
		t.Error("request body is nil")
	}
}

// Sanity: ensure the webpush-go API surface we depend on still exists at
// compile time. If this stops compiling, the dependency made a breaking
// change and the production Send code needs updating.
var _ webpush.HTTPClient = (*http.Client)(nil)
var _ = url.Parse
