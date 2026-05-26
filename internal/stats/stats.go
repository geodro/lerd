// Package stats reads cheap per-container resource usage via `podman stats`
// and exposes a structured view both lerd-ui (for the dashboard widget) and
// the TUI (for its Dashboard pane) can share. Lives outside internal/ui so
// the TUI can call it in-process without pulling in the HTTP server stack.
package stats

import (
	"context"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/podman"
)

// ContainerStat is one row of resource usage for a single lerd-prefixed
// container. Mirrors the JSON the web UI consumes so callers can serialize
// directly without an adapter struct.
type ContainerStat struct {
	Name       string  `json:"name"`
	CPUPercent float64 `json:"cpu_percent"`
	MemBytes   int64   `json:"mem_bytes"`
	MemLimit   int64   `json:"mem_limit_bytes"`
	MemPercent float64 `json:"mem_percent"`
}

// Snapshot is the aggregated view returned by Read. Totals are summed
// server-side so multiple subscribers don't each re-aggregate.
type Snapshot struct {
	Containers      []ContainerStat `json:"containers"`
	TotalCPUPercent float64         `json:"total_cpu_percent"`
	TotalMemBytes   int64           `json:"total_mem_bytes"`
	HostMemBytes    int64           `json:"host_mem_bytes"`
	UpdatedAt       time.Time       `json:"updated_at"`
	Available       bool            `json:"available"`
}

// readerFn is swappable for tests so callers don't need a live podman.
var readerFn = readPodmanStats

const readTimeout = 4 * time.Second

// Read returns a fresh snapshot. Callers that need caching wrap this with
// their own TTL (lerd-ui caches for 3s in handleStats; the TUI dashboard
// holds the last snapshot between frames).
func Read() Snapshot {
	out := Snapshot{
		Containers: []ContainerStat{},
		UpdatedAt:  time.Now(),
	}
	rows, err := readerFn()
	if err != nil || len(rows) == 0 {
		return out
	}
	out.Available = true
	out.Containers = rows
	for _, r := range rows {
		out.TotalCPUPercent += r.CPUPercent
		out.TotalMemBytes += r.MemBytes
		if r.MemLimit > out.HostMemBytes {
			out.HostMemBytes = r.MemLimit
		}
	}
	sort.Slice(out.Containers, func(i, j int) bool {
		return out.Containers[i].MemBytes > out.Containers[j].MemBytes
	})
	return out
}

// cached wraps Read with a TTL cache so multiple callers in the same process
// (TUI: dashboard pane redraws every frame; lerd-ui: many open dashboards)
// don't each pay the podman cost. The `inflight` channel singleflights
// concurrent refreshes: the first caller after expiry takes the cost,
// every other caller waits on the same channel and reads the new value
// once it lands. Without this, N parallel callers all observe stale at
// the same instant and each spawn `podman stats`, multiplying the cost.
type cached struct {
	mu       sync.Mutex
	value    *Snapshot
	at       time.Time
	inflight chan struct{}
}

var defaultCache = &cached{}

// Cached returns Read's result, refreshing at most once per ttl across all
// concurrent callers. Safe for concurrent use: the first caller after the
// TTL expires runs Read; later callers wait for that single Read to finish
// (or read the now-fresh cached value on retry).
func Cached(ttl time.Duration) Snapshot {
	for {
		defaultCache.mu.Lock()
		// Fresh value wins — return a copy.
		if defaultCache.value != nil && time.Since(defaultCache.at) < ttl {
			v := *defaultCache.value
			defaultCache.mu.Unlock()
			return v
		}
		// Another goroutine is already refreshing — wait, then loop and
		// pick up the now-fresh value.
		if defaultCache.inflight != nil {
			ch := defaultCache.inflight
			defaultCache.mu.Unlock()
			<-ch
			continue
		}
		// We're the elected refresher; broadcast our intent by storing
		// the channel and releasing the lock for the duration of Read.
		done := make(chan struct{})
		defaultCache.inflight = done
		defaultCache.mu.Unlock()

		snap := Read()

		defaultCache.mu.Lock()
		defaultCache.value = &snap
		defaultCache.at = time.Now()
		defaultCache.inflight = nil
		defaultCache.mu.Unlock()
		close(done)
		return snap
	}
}

// SetReader swaps the underlying reader for tests so callers can drive Read
// from a fixture without shelling out to podman.
func SetReader(fn func() ([]ContainerStat, error)) (restore func()) {
	prev := readerFn
	readerFn = fn
	return func() { readerFn = prev }
}

// readPodmanStats invokes `podman stats --no-stream` with a pipe-delimited
// template. Filters to containers prefixed `lerd-` so we never accidentally
// surface unrelated containers on the host.
func readPodmanStats() ([]ContainerStat, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		podman.PodmanBin(), "stats", "--no-stream",
		"--format", "{{.Name}}|{{.CPU}}|{{.MemUsage}}|{{.MemPerc}}",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return ParseRows(string(out)), nil
}

// ParseRows turns the output of `podman stats --no-stream --format …` into a
// slice of ContainerStat. Exported so tests for either caller can build
// inputs without a real podman.
func ParseRows(text string) []ContainerStat {
	var rows []ContainerStat
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 4 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if !strings.HasPrefix(name, "lerd-") {
			continue
		}
		cpu, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		used, limit := parseMemUsage(parts[2])
		memPerc, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[3]), "%"), 64)
		rows = append(rows, ContainerStat{
			Name:       name,
			CPUPercent: cpu,
			MemBytes:   used,
			MemLimit:   limit,
			MemPercent: memPerc,
		})
	}
	return rows
}

func parseMemUsage(s string) (used, limit int64) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	return parseSize(parts[0]), parseSize(parts[1])
}

func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	splitAt := len(s)
	for i, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != '+' && r != 'e' && r != 'E' {
			splitAt = i
			break
		}
	}
	num, err := strconv.ParseFloat(strings.TrimSpace(s[:splitAt]), 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(strings.TrimSpace(s[splitAt:]))
	mult := float64(1)
	switch unit {
	case "", "b":
		mult = 1
	case "k", "kb", "kib":
		mult = 1024
	case "m", "mb", "mib":
		mult = 1024 * 1024
	case "g", "gb", "gib":
		mult = 1024 * 1024 * 1024
	case "t", "tb", "tib":
		mult = 1024 * 1024 * 1024 * 1024
	}
	return int64(num * mult)
}

// FormatBytes turns a byte count into a short human string ("128MB",
// "2.4GB"). Used by both the TUI dashboard and lerd-ui's resources widget
// so the units they show match.
func FormatBytes(b int64) string {
	const k = 1024
	switch {
	case b < k:
		return strconv.FormatInt(b, 10) + "B"
	case b < k*k:
		return strconv.FormatFloat(float64(b)/k, 'f', 0, 64) + "KB"
	case b < k*k*k:
		return strconv.FormatFloat(float64(b)/(k*k), 'f', 0, 64) + "MB"
	default:
		return strconv.FormatFloat(float64(b)/(k*k*k), 'f', 1, 64) + "GB"
	}
}
