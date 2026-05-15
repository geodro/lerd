package ui

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/push"
)

// mailpitWebhookPayload is the subset of Mailpit's message-summary JSON we
// surface to the browser. Mailpit POSTs the full /api/v1/message/{id} schema
// to MP_WEBHOOK_URL for every accepted message; we only care about enough
// fields to render a one-line desktop notification and deep-link back.
type mailpitWebhookPayload struct {
	ID      string             `json:"ID"`
	Subject string             `json:"Subject"`
	From    mailpitAddress     `json:"From"`
	To      []mailpitAddress   `json:"To"`
	Created mailpitCreatedTime `json:"Created"`
}

type mailpitAddress struct {
	Name    string `json:"Name"`
	Address string `json:"Address"`
}

// mailpitCreatedTime accepts either RFC3339 strings or an ISO-with-millis
// shape, both of which Mailpit has emitted across recent versions. Empty or
// unparseable values fall back to "now" so the notification still fires.
type mailpitCreatedTime struct{ t time.Time }

func (c *mailpitCreatedTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		c.t = time.Now()
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			c.t = t
			return nil
		}
	}
	c.t = time.Now()
	return nil
}

// handleMailpitWebhook receives Mailpit's per-message POST and builds a
// generic push.Notification that dispatchNotification fans out to both the
// websocket and to subscribed browsers via Web Push. The endpoint is
// reachable from inside the mailpit container via host.containers.internal
// (see the mailpit preset).
func handleMailpitWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	var p mailpitWebhookPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&p); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if p.ID == "" {
		http.Error(w, "missing message id", http.StatusBadRequest)
		return
	}

	from := p.From.Address
	if p.From.Name != "" && from != "" {
		from = p.From.Name + " <" + p.From.Address + ">"
	} else if from == "" {
		from = p.From.Name
	}
	subject := strings.TrimSpace(p.Subject)
	if subject == "" {
		subject = "(no subject)"
	}
	if from == "" {
		from = "(unknown sender)"
	}

	dispatchNotification(push.Notification{
		Kind:     "mail",
		TitleKey: "notify_mail_title",
		Title:    "New email: " + subject,
		BodyKey:  "notify_mail_body",
		Body:     "From: " + from,
		Params: map[string]string{
			"subject": subject,
			"from":    from,
		},
		Tag:  "lerd-mail-" + p.ID,
		URL:  "#service/mailpit/view/" + p.ID,
		Icon: "/icons/icon-192.png",
		Data: map[string]string{"id": p.ID},
		TTL:  60,
	})

	w.WriteHeader(http.StatusNoContent)
}
