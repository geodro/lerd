//go:build !nogui

package tray

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestSchemeIsLight(t *testing.T) {
	cases := map[uint32]bool{
		0: false, // no preference
		1: false, // prefer dark
		2: true,  // prefer light
		3: false, // unknown
	}
	for scheme, want := range cases {
		if got := schemeIsLight(scheme); got != want {
			t.Errorf("schemeIsLight(%d) = %v, want %v", scheme, got, want)
		}
	}
}

func TestParseColorScheme(t *testing.T) {
	tests := []struct {
		name   string
		value  interface{}
		want   uint32
		wantOK bool
	}{
		{"plain uint32", uint32(2), 2, true},
		{"single variant", dbus.MakeVariant(uint32(1)), 1, true},
		{"double variant", dbus.MakeVariant(dbus.MakeVariant(uint32(2))), 2, true},
		{"int32", int32(2), 2, true},
		{"non-numeric", "light", 0, false},
		{"nil", nil, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseColorScheme(tc.value)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("parseColorScheme(%v) = (%d, %v), want (%d, %v)", tc.value, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestSignalColorScheme(t *testing.T) {
	tests := []struct {
		name      string
		body      []interface{}
		wantLight bool
		wantOK    bool
	}{
		{
			name:      "light preference",
			body:      []interface{}{appearanceNS, colorSchemeKey, dbus.MakeVariant(uint32(2))},
			wantLight: true,
			wantOK:    true,
		},
		{
			name:      "dark preference",
			body:      []interface{}{appearanceNS, colorSchemeKey, dbus.MakeVariant(uint32(1))},
			wantLight: false,
			wantOK:    true,
		},
		{
			name:   "unrelated namespace",
			body:   []interface{}{"org.gnome.desktop.interface", colorSchemeKey, dbus.MakeVariant(uint32(2))},
			wantOK: false,
		},
		{
			name:   "unrelated key",
			body:   []interface{}{appearanceNS, "accent-color", dbus.MakeVariant(uint32(2))},
			wantOK: false,
		},
		{
			name:   "short body",
			body:   []interface{}{appearanceNS, colorSchemeKey},
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			light, ok := signalColorScheme(&dbus.Signal{Body: tc.body})
			if ok != tc.wantOK || light != tc.wantLight {
				t.Errorf("signalColorScheme = (%v, %v), want (%v, %v)", light, ok, tc.wantLight, tc.wantOK)
			}
		})
	}
}

func TestSignalColorSchemeNil(t *testing.T) {
	if _, ok := signalColorScheme(nil); ok {
		t.Error("signalColorScheme(nil) should not be ok")
	}
}
