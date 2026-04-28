//go:build linux

package systemd

import (
	"context"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"
)

// SubscribeLerdUnitStateChanges wires a push-based notification for every
// systemd unit whose name matches "lerd-*". onChange fires with the bare
// unit name (no .service suffix) whenever the unit's SubState changes,
// which covers start, stop, restart, activating, and failed transitions.
// Cancel ctx to tear down the subscription.
//
// The handler goroutine keeps running until ctx is cancelled; callers pass
// a long-lived context (server lifetime) because state changes need to be
// delivered continuously. Calling this more than once per process is safe
// but wasteful: each call starts its own fanout goroutine.
func SubscribeLerdUnitStateChanges(ctx context.Context, onChange func(unitName string)) error {
	conn, err := userConn()
	if err != nil {
		return err
	}
	if err := conn.Subscribe(); err != nil {
		return err
	}

	updateCh := make(chan *dbus.SubStateUpdate, 64)
	errCh := make(chan error, 8)
	conn.SetSubStateSubscriber(updateCh, errCh)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case upd := <-updateCh:
				if upd == nil {
					continue
				}
				name := strings.TrimSuffix(upd.UnitName, ".service")
				if !strings.HasPrefix(name, "lerd-") {
					continue
				}
				onChange(name)
			case <-errCh:
				// Drop subscription errors rather than spamming; the UI
				// poll fallback keeps state current even if we lose pushes.
			}
		}
	}()
	return nil
}
