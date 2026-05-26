package ui

import (
	"net/http"
	"time"

	"github.com/geodro/lerd/internal/stats"
)

// statsCacheTTL is the freshness window the dashboard polls against. podman
// stats --no-stream takes ~100-300ms; with many open tabs (web dashboard,
// System tab, TUI) all calling at once, a 3s TTL keeps host load negligible.
const statsCacheTTL = 3 * time.Second

// handleStats returns the latest container stats via the shared
// internal/stats cache. Kept as a thin wrapper so the JSON shape exposed
// to the web UI stays stable while the parsing logic lives in one place.
func handleStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, stats.Cached(statsCacheTTL))
}
