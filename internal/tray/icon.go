//go:build !nogui

package tray

import _ "embed"

//go:embed assets/icon.png
var iconPNG []byte

//go:embed assets/icon-white.png
var iconWhitePNG []byte

//go:embed assets/icon-mono.png
var iconMonoPNG []byte
