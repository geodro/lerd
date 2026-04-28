//go:build linux

package systemd

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/login1"
	"github.com/godbus/dbus/v5"
)

// login1Conn holds the lazily-initialised systemd-logind system-bus conn.
// Read-only property polls don't need thread-local connections, so one
// long-lived conn serves every watcher/UI caller in this process.
var (
	login1Once sync.Once
	login1C    *login1.Conn
	login1Err  error
)

func login1Connect() (*login1.Conn, error) {
	login1Once.Do(func() {
		login1C, login1Err = login1.New()
	})
	return login1C, login1Err
}

// SessionIsIdle returns true when systemd-logind reports the current user's
// active session as idle (the compositor hasn't seen input for the
// configured idle timeout). Returns false when logind is unreachable or
// the session can't be identified, so callers behave as if the user is
// active when the signal is unavailable.
func SessionIsIdle() bool {
	c, err := login1Connect()
	if err != nil {
		return false
	}
	path, err := resolveOwnSessionPath(c)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := c.GetSessionPropertyContext(ctx, path, "IdleHint")
	if err != nil || v == nil {
		return false
	}
	b, ok := v.Value().(bool)
	return ok && b
}

// SessionIsLocked returns true when the user's session is locked by the
// screen locker. Returns false when the property is unreadable.
func SessionIsLocked() bool {
	c, err := login1Connect()
	if err != nil {
		return false
	}
	path, err := resolveOwnSessionPath(c)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := c.GetSessionPropertyContext(ctx, path, "LockedHint")
	if err != nil || v == nil {
		return false
	}
	b, ok := v.Value().(bool)
	return ok && b
}

// resolveOwnSessionPath prefers the session exposed in XDG_SESSION_ID and
// falls back to the user's active session so headless systemd-user timers
// without that env var still resolve to something useful.
func resolveOwnSessionPath(c *login1.Conn) (dbus.ObjectPath, error) {
	if id := os.Getenv("XDG_SESSION_ID"); id != "" {
		if p, err := c.GetSession(id); err == nil {
			return p, nil
		}
	}
	uid := uint32(os.Getuid())
	users, err := c.ListUsers()
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if u.UID == uid {
			return c.GetActiveSession()
		}
	}
	return "", errors.New("no active login1 session for current user")
}

// SessionIsIdleOrLocked is a convenience combining both states.
func SessionIsIdleOrLocked() bool {
	return SessionIsIdle() || SessionIsLocked()
}
