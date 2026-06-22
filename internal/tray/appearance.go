//go:build !nogui

package tray

import (
	"context"

	"github.com/godbus/dbus/v5"
)

// The XDG desktop portal exposes the user's light/dark preference in a
// DE-agnostic way (GNOME, KDE, and anything else implementing the portal),
// both as a readable setting and a live SettingChanged signal.
const (
	portalDest     = "org.freedesktop.portal.Desktop"
	settingsIface  = "org.freedesktop.portal.Settings"
	appearanceNS   = "org.freedesktop.appearance"
	colorSchemeKey = "color-scheme"
)

const portalPath = dbus.ObjectPath("/org/freedesktop/portal/desktop")

// schemeIsLight maps the XDG color-scheme value to a light/dark panel. The spec
// defines 0 = no preference, 1 = prefer dark, 2 = prefer light. Anything other
// than an explicit light preference keeps the existing white icon, which is the
// right default for the common dark-panel case and for unknown values.
func schemeIsLight(scheme uint32) bool {
	return scheme == 2
}

// parseColorScheme unwraps the nested variants the portal returns and pulls out
// the numeric color-scheme value.
func parseColorScheme(v interface{}) (uint32, bool) {
	for {
		variant, ok := v.(dbus.Variant)
		if !ok {
			break
		}
		v = variant.Value()
	}
	switch n := v.(type) {
	case uint32:
		return n, true
	case int32:
		return uint32(n), true
	case uint8:
		return uint32(n), true
	}
	return 0, false
}

// signalColorScheme extracts the light/dark state from a SettingChanged signal,
// returning ok=false for signals about unrelated settings.
func signalColorScheme(sig *dbus.Signal) (light bool, ok bool) {
	if sig == nil || len(sig.Body) < 3 {
		return false, false
	}
	ns, _ := sig.Body[0].(string)
	key, _ := sig.Body[1].(string)
	if ns != appearanceNS || key != colorSchemeKey {
		return false, false
	}
	scheme, ok := parseColorScheme(sig.Body[2])
	if !ok {
		return false, false
	}
	return schemeIsLight(scheme), true
}

func readColorScheme(conn *dbus.Conn) (uint32, bool) {
	var out dbus.Variant
	call := conn.Object(portalDest, portalPath).Call(settingsIface+".Read", 0, appearanceNS, colorSchemeKey)
	if call.Err != nil {
		return 0, false
	}
	if err := call.Store(&out); err != nil {
		return 0, false
	}
	return parseColorScheme(out)
}

// watchAppearance reports whether the desktop panel is light, once on startup
// and again every time the user flips their light/dark preference. It is a
// no-op on systems without an XDG desktop portal (older or headless setups),
// which leaves the existing white running icon in place.
func watchAppearance(ctx context.Context, onChange func(light bool)) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return
	}
	if scheme, ok := readColorScheme(conn); ok {
		onChange(schemeIsLight(scheme))
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(portalPath),
		dbus.WithMatchInterface(settingsIface),
		dbus.WithMatchMember("SettingChanged"),
	); err != nil {
		conn.Close()
		return
	}
	sigCh := make(chan *dbus.Signal, 8)
	conn.Signal(sigCh)
	go func() {
		defer conn.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-sigCh:
				if !ok {
					return
				}
				if light, ok := signalColorScheme(sig); ok {
					onChange(light)
				}
			}
		}
	}()
}
