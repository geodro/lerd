package push

import (
	"fmt"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// HTTPClient is the http.Client webpush-go uses; tests swap in a recorder.
var HTTPClient = &http.Client{Timeout: 10 * time.Second}

const defaultTTL = 60

// Send fans the notification out to every stored subscription and prunes
// any subscription the push service has retired (404 / 410). Single
// entry point: no parallel SendBytes/SendAll exists.
func Send(n Notification) error {
	payload, err := n.Payload()
	if err != nil {
		return fmt.Errorf("encoding notification: %w", err)
	}
	priv, pub, err := VAPIDKeys()
	if err != nil {
		return err
	}
	subs, err := List()
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}

	ttl := n.TTL
	if ttl == 0 {
		ttl = defaultTTL
	}
	urgency := webpush.Urgency(n.Urgency)
	if urgency == "" {
		urgency = webpush.UrgencyNormal
	}

	for _, s := range subs {
		if !s.Allows(n.Kind) {
			continue
		}
		ws := &webpush.Subscription{
			Endpoint: s.Endpoint,
			Keys:     webpush.Keys{Auth: s.Auth, P256dh: s.P256dh},
		}
		resp, err := webpush.SendNotification(payload, ws, &webpush.Options{
			HTTPClient:      HTTPClient,
			Subscriber:      VAPIDSubject,
			VAPIDPublicKey:  pub,
			VAPIDPrivateKey: priv,
			TTL:             ttl,
			Urgency:         urgency,
		})
		if err != nil {
			fmt.Printf("[push] send to %s: %v\n", endpointShort(s.Endpoint), err)
			continue
		}
		if resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
				if err := Remove(s.Endpoint); err != nil {
					fmt.Printf("[push] removing stale subscription: %v\n", err)
				}
				continue
			}
			if resp.StatusCode >= 400 {
				fmt.Printf("[push] %s returned %d\n", endpointShort(s.Endpoint), resp.StatusCode)
			}
		}
	}
	return nil
}

// endpointShort strips the per-install secret token from the endpoint URL
// so logs only show the push service host, not the bearer-equivalent path.
func endpointShort(endpoint string) string {
	for i := len("https://"); i < len(endpoint); i++ {
		if endpoint[i] == '/' {
			return endpoint[:i]
		}
	}
	return endpoint
}
