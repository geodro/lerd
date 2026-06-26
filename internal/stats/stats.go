// Package stats reads cheap per-container resource usage via `podman stats`
// and exposes a structured view both lerd-ui (for the dashboard widget) and
// the TUI (for its Dashboard pane) can share. Lives outside internal/ui so
// the TUI can call it in-process without pulling in the HTTP server stack.
package stats

import (
	"bufio"
	"context"
	"runtime"
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

// readerFn (containers, via podman) and hostReaderFn (lerd's own host-side
// processes, via systemd accounting) are swappable for tests so callers don't
// need a live podman or systemd.
var readerFn = readPodmanStats
var hostReaderFn = readHostProcesses

// numCPU reports the host core count used to normalize the per-core CPU sum into a
// host fraction. A function var so tests can pin it and assert deterministically
// regardless of the machine they run on.
var numCPU = runtime.NumCPU

const readTimeout = 6 * time.Second

// Read returns a fresh snapshot. Callers that need caching wrap this with
// their own TTL (lerd-ui caches for 3s in handleStats; the TUI dashboard
// holds the last snapshot between frames).
func Read() Snapshot {
	out := Snapshot{
		Containers: []ContainerStat{},
		UpdatedAt:  time.Now(),
	}

	// Containers (podman, ~3s stream) and host processes (systemd accounting,
	// ~1s sample) are read concurrently so the host read hides under the longer
	// podman one and adds no wall time.
	var containers, hosts []ContainerStat
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); containers, _ = readerFn() }()
	go func() { defer wg.Done(); hosts, _ = hostReaderFn() }()
	wg.Wait()

	rows := containers
	// A container's quadlet unit (lerd-mysql.service, …) also surfaces in the
	// host list; drop it so podman's measurement wins and it isn't counted twice.
	isContainer := make(map[string]bool, len(containers))
	for _, c := range containers {
		isContainer[c.Name] = true
	}
	for _, h := range hosts {
		if !isContainer[h.Name] {
			rows = append(rows, h)
		}
	}
	if len(rows) == 0 {
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
	// Rank by combined load — each container's share of the total CPU plus its
	// share of the total memory — so the top-N the dashboard shows reflects both
	// headline numbers. Sorting by memory alone hid the containers actually
	// driving CPU (a busy php-fpm uses little RAM), making the CPU total look
	// unexplained against the listed consumers. Memory breaks ties.
	score := func(c ContainerStat) float64 {
		var s float64
		if out.TotalCPUPercent > 0 {
			s += c.CPUPercent / out.TotalCPUPercent
		}
		if out.TotalMemBytes > 0 {
			s += float64(c.MemBytes) / float64(out.TotalMemBytes)
		}
		return s
	}
	sort.Slice(out.Containers, func(i, j int) bool {
		si, sj := score(out.Containers[i]), score(out.Containers[j])
		if si != sj {
			return si > sj
		}
		return out.Containers[i].MemBytes > out.Containers[j].MemBytes
	})
	// Each row's CPU% is normalized to a single core (podman stats and the host
	// systemd-accounting reader both report per-core), so the raw sum is per-core
	// and on a multi-core box can far exceed 100%. The headline frames it as a
	// share of the whole host (like the memory total), so divide by the core count
	// to turn the per-core sum into a true host fraction. Per-row CPU% stays
	// per-core, the intuitive "this process is pegging one core" reading.
	if cores := numCPU(); cores > 1 {
		out.TotalCPUPercent /= float64(cores)
	}
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

// SetReader swaps the underlying container reader for tests so callers can drive
// Read from a fixture without shelling out to podman.
func SetReader(fn func() ([]ContainerStat, error)) (restore func()) {
	prev := readerFn
	readerFn = fn
	return func() { readerFn = prev }
}

// SetHostReader swaps the host-process reader for tests so Read doesn't shell out
// to systemctl (which would also add the ~1s CPU-sample sleep).
func SetHostReader(fn func() ([]ContainerStat, error)) (restore func()) {
	prev := hostReaderFn
	hostReaderFn = fn
	return func() { hostReaderFn = prev }
}

// readPodmanStats streams `podman stats` with a pipe-delimited template and
// returns one row per `lerd-`-prefixed container using each container's SECOND
// sample. podman's first CPU sample is the average over the container's whole
// lifetime, not its current load, so a long-lived container that was busy at
// startup (FPM/opcache warmup) would read as permanently busy. The second
// sample is the real instantaneous rate (a delta over `--interval`). Streaming
// costs ~2s versus the old instant `--no-stream`, which the caller's TTL cache
// absorbs.
func readPodmanStats() ([]ContainerStat, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	cmd := podman.CmdContext(
		ctx,
		"stats", "--interval", "1",
		"--format", "{{.Name}}|{{.CPU}}|{{.MemUsage}}|{{.MemPerc}}",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// podman prints every container once per interval in a stable order, so the
	// third sighting of any container means all of them now have at least two
	// samples — enough to stop and take the instantaneous values.
	counts := map[string]int{}
	var lines []string
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		stat, ok := parseStatLine(line)
		if !ok {
			continue
		}
		lines = append(lines, line)
		counts[stat.Name]++
		if counts[stat.Name] >= 3 {
			break
		}
	}
	return secondCycleStats(lines), nil
}

// secondCycleStats collapses streamed `podman stats` lines into one row per
// container, preferring each container's second sample (the instantaneous rate)
// and falling back to the first only when the stream was cut short before a
// second arrived, so a container is never dropped entirely. First-seen order is
// preserved; Read re-sorts for display.
func secondCycleStats(lines []string) []ContainerStat {
	type samples struct{ first, second *ContainerStat }
	seen := map[string]*samples{}
	var order []string
	for _, line := range lines {
		stat, ok := parseStatLine(line)
		if !ok {
			continue
		}
		s := stat
		a := seen[s.Name]
		if a == nil {
			a = &samples{}
			seen[s.Name] = a
			order = append(order, s.Name)
		}
		switch {
		case a.first == nil:
			a.first = &s
		case a.second == nil:
			a.second = &s
		}
	}
	rows := make([]ContainerStat, 0, len(order))
	for _, name := range order {
		a := seen[name]
		if a.second != nil {
			rows = append(rows, *a.second)
		} else if a.first != nil {
			rows = append(rows, *a.first)
		}
	}
	return rows
}

// ParseRows turns multi-line `podman stats --format …` output into a slice of
// ContainerStat. Exported so tests for either caller can build inputs without a
// real podman.
func ParseRows(text string) []ContainerStat {
	var rows []ContainerStat
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if stat, ok := parseStatLine(line); ok {
			rows = append(rows, stat)
		}
	}
	return rows
}

// parseStatLine parses one `{{.Name}}|{{.CPU}}|{{.MemUsage}}|{{.MemPerc}}` line,
// returning ok=false for blanks, malformed rows, and non-`lerd-` containers (so
// unrelated host containers never surface).
func parseStatLine(line string) (ContainerStat, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return ContainerStat{}, false
	}
	parts := strings.Split(line, "|")
	if len(parts) != 4 {
		return ContainerStat{}, false
	}
	name := strings.TrimSpace(parts[0])
	if !strings.HasPrefix(name, "lerd-") {
		return ContainerStat{}, false
	}
	cpu, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	used, limit := parseMemUsage(parts[2])
	memPerc, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[3]), "%"), 64)
	return ContainerStat{
		Name:       name,
		CPUPercent: cpu,
		MemBytes:   used,
		MemLimit:   limit,
		MemPercent: memPerc,
	}, true
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
