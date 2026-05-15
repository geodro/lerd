package ui

import (
	"encoding/json"
	"net/http"

	"github.com/geodro/lerd/internal/push"
)

// handlePushVAPIDPublicKey returns the per-install VAPID public key for
// pushManager.subscribe's applicationServerKey.
func handlePushVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pub, err := push.VAPIDPublicKey()
	if err != nil {
		http.Error(w, "vapid: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"public_key": pub})
}

// handlePushSubscribe stores a browser-supplied PushSubscription plus its
// per-category prefs. Re-POSTing with the same endpoint is idempotent —
// it replaces the prior entry so prefs stay in sync with the client.
func handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
		Enabled      *bool    `json:"enabled,omitempty"`
		EnabledKinds []string `json:"enabled_kinds,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid subscription", http.StatusBadRequest)
		return
	}
	if body.Endpoint == "" || body.Keys.P256dh == "" || body.Keys.Auth == "" {
		http.Error(w, "subscription missing endpoint/p256dh/auth", http.StatusBadRequest)
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	sub := push.Subscription{
		Endpoint:     body.Endpoint,
		P256dh:       body.Keys.P256dh,
		Auth:         body.Keys.Auth,
		UA:           r.Header.Get("User-Agent"),
		Enabled:      enabled,
		EnabledKinds: body.EnabledKinds,
	}
	if err := push.Add(sub); err != nil {
		http.Error(w, "store error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if err := push.Remove(body.Endpoint); err != nil {
		http.Error(w, "store error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePushDevices returns a sanitised view of every stored subscription
// (UA, added_at, prefs, but never P256dh or Auth) for the settings panel's
// "Subscribed devices" list.
func handlePushDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	subs, err := push.List()
	if err != nil {
		http.Error(w, "store error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	type pubDevice struct {
		Endpoint     string   `json:"endpoint"`
		UA           string   `json:"ua"`
		AddedAt      int64    `json:"added_at"`
		Enabled      bool     `json:"enabled"`
		EnabledKinds []string `json:"enabled_kinds,omitempty"`
	}
	out := make([]pubDevice, 0, len(subs))
	for _, s := range subs {
		out = append(out, pubDevice{
			Endpoint:     s.Endpoint,
			UA:           s.UA,
			AddedAt:      s.AddedAt,
			Enabled:      s.Enabled,
			EnabledKinds: s.EnabledKinds,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// handlePushTest dispatches a hard-coded notification through the central
// notifier so the user can verify the full stack (WS broadcast + Web Push +
// SW handler) from the settings panel.
func handlePushTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dispatchNotification(push.Notification{
		Kind:     "test",
		TitleKey: "notify_test_title",
		Title:    "lerd notifications test",
		BodyKey:  "notify_test_body",
		Body:     "If you see this, push notifications are working.",
		Tag:      "lerd-test",
		URL:      "#system",
		Icon:     "/icons/icon-192.png",
		TTL:      60,
		Urgency:  "normal",
	})
	w.WriteHeader(http.StatusNoContent)
}
