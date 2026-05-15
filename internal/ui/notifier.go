package ui

import (
	"fmt"

	"github.com/geodro/lerd/internal/push"
)

// dispatchNotification is the single choke point for emitting notifications.
// Every producer (mailpit webhook, worker watcher, op-done subscriber,
// dumps subscriber, service-update diff) routes through here.
func dispatchNotification(n push.Notification) {
	payload, err := n.Payload()
	if err != nil {
		return
	}
	broker.broadcastNotification(payload)
	go func() {
		if err := push.Send(n); err != nil {
			fmt.Printf("[notifier] push send failed: %v\n", err)
		}
	}()
}
