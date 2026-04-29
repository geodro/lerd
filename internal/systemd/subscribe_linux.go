//go:build linux

package systemd

import (
	"context"
	"strings"
	"sync"

	"github.com/coreos/go-systemd/v22/dbus"
)

// UnitStateWatcher manages the DBus SubState subscription. The subscription
// is expensive at idle: go-systemd's dispatch goroutine receives a
// PropertiesChanged signal for every property of every active unit and calls
// GetUnitProperties on each, which on a host with many lerd-* units burns a
// noticeable chunk of CPU even when nobody is watching the dashboard.
//
// To pay that cost only when it produces value, the daemon starts the
// watcher when a UI tab connects and stops it when the last one disconnects.
// Stop closes a dedicated DBus connection so the underlying signal traffic
// stops at the systemd side rather than just being dropped on our side.
type UnitStateWatcher struct {
	mu       sync.Mutex
	conn     *dbus.Conn
	cancel   context.CancelFunc
	onChange func(unitName string)
}

// NewUnitStateWatcher returns a watcher that calls onChange on every SubState
// transition of a lerd-* unit while running. The watcher starts in the
// stopped state; call Start to begin receiving notifications.
func NewUnitStateWatcher(onChange func(unitName string)) *UnitStateWatcher {
	return &UnitStateWatcher{onChange: onChange}
}

// Start begins receiving SubState notifications. Calling Start while already
// running is a no-op. Each Start opens its own DBus connection so a paired
// Stop can close it cleanly.
func (w *UnitStateWatcher) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn != nil {
		return nil
	}

	conn, err := dbus.NewUserConnectionContext(context.Background())
	if err != nil {
		return err
	}
	if err := conn.Subscribe(); err != nil {
		conn.Close()
		return err
	}

	updateCh := make(chan *dbus.SubStateUpdate, 64)
	errCh := make(chan error, 8)
	conn.SetSubStateSubscriber(updateCh, errCh)

	ctx, cancel := context.WithCancel(context.Background())
	w.conn = conn
	w.cancel = cancel

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
				w.onChange(name)
			case <-errCh:
				// Drop subscription errors; the periodic podman cache poll
				// keeps state current even when pushes are missed.
			}
		}
	}()
	return nil
}

// Stop tears down the DBus subscription so go-systemd's dispatch goroutine
// stops fetching unit properties. Safe to call when already stopped.
func (w *UnitStateWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn == nil {
		return
	}
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	// Tell systemd to stop emitting unit signals on this connection, then
	// close the connection so the dispatch goroutine inside go-systemd
	// returns instead of looping on a now-quiet but still-allocated socket.
	_ = w.conn.Unsubscribe()
	w.conn.Close()
	w.conn = nil
}

// SubscribeLerdUnitStateChanges keeps the previous one-shot subscription
// helper for non-UI callers (e.g. tests). New code should prefer the
// stoppable UnitStateWatcher.
func SubscribeLerdUnitStateChanges(ctx context.Context, onChange func(unitName string)) error {
	w := NewUnitStateWatcher(onChange)
	if err := w.Start(); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		w.Stop()
	}()
	return nil
}
