package stats

import (
	"sync/atomic"
	"testing"
	"time"
)

func mb(n float64) int64 { return int64(n * 1048576) }
func gb(n float64) int64 { return int64(n * 1073741824) }

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"45.32MB", mb(45.32)},
		{"539.3MB", mb(539.3)},
		{"33.23GB", gb(33.23)},
		{"7.369MB", mb(7.369)},
		{"  191.3 MB  ", mb(191.3)},
		{"1024", 1024},
		{"", 0},
		{"garbage", 0},
	}
	for _, c := range cases {
		if got := parseSize(c.in); got != c.want {
			t.Errorf("parseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseMemUsage(t *testing.T) {
	used, limit := parseMemUsage("45.32MB / 33.23GB")
	if used != mb(45.32) {
		t.Errorf("used = %d", used)
	}
	if limit != gb(33.23) {
		t.Errorf("limit = %d", limit)
	}

	if u, l := parseMemUsage("malformed"); u != 0 || l != 0 {
		t.Errorf("malformed = %d %d", u, l)
	}
}

func TestParseRows_FiltersToLerdContainers(t *testing.T) {
	in := `lerd-mysql|0.115|75.53MB / 33.23GB|0.23%
some-other-container|0.5|10MB / 33GB|0.03%
lerd-redis|0.001|5.2MB / 33.23GB|0.02%
`
	rows := ParseRows(in)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Name != "lerd-mysql" || rows[1].Name != "lerd-redis" {
		t.Errorf("names: %v", []string{rows[0].Name, rows[1].Name})
	}
	if rows[0].CPUPercent != 0.115 {
		t.Errorf("cpu = %v", rows[0].CPUPercent)
	}
	if rows[0].MemBytes != mb(75.53) {
		t.Errorf("mem = %d", rows[0].MemBytes)
	}
	if rows[0].MemPercent != 0.23 {
		t.Errorf("mem percent = %v", rows[0].MemPercent)
	}
}

func TestParseRows_SkipsMalformedLines(t *testing.T) {
	in := `lerd-mysql|0.115|75.53MB / 33.23GB|0.23%

lerd-redis|incomplete
lerd-postgres|0.5|10MB / 33GB|0.03%
`
	rows := ParseRows(in)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (skipping blank + malformed)", len(rows))
	}
}

// Read ranks containers by combined CPU+memory load so the dashboard's top-N
// reflects both headline totals. A CPU-heavy container with little memory (a busy
// php-fpm) must outrank a memory-heavy idle one; a pure memory sort hid it, which
// made the CPU total look unexplained against the listed consumers.
// noHostProcesses stubs the host-process reader so Read tests stay hermetic and
// fast (no systemctl, no CPU-sample sleep).
func noHostProcesses(t *testing.T) {
	t.Cleanup(SetHostReader(func() ([]ContainerStat, error) { return nil, nil }))
}

func TestRead_SortsByCombinedLoad(t *testing.T) {
	noHostProcesses(t)
	restore := SetReader(func() ([]ContainerStat, error) {
		return []ContainerStat{
			{Name: "lerd-memhog", CPUPercent: 0.1, MemBytes: 500_000_000, MemLimit: 33_000_000_000, MemPercent: 1.5},
			{Name: "lerd-cpuhog", CPUPercent: 5.0, MemBytes: 10_000_000, MemLimit: 33_000_000_000, MemPercent: 0.03},
			{Name: "lerd-small", CPUPercent: 0.1, MemBytes: 20_000_000, MemLimit: 33_000_000_000, MemPercent: 0.06},
		}, nil
	})
	t.Cleanup(restore)

	resp := Read()
	if !resp.Available {
		t.Fatal("expected Available=true with non-empty data")
	}
	if len(resp.Containers) != 3 {
		t.Fatalf("got %d containers", len(resp.Containers))
	}
	if resp.Containers[0].Name != "lerd-cpuhog" {
		t.Errorf("CPU-heavy low-memory container should rank first by combined load; got %q", resp.Containers[0].Name)
	}
	if resp.Containers[1].Name != "lerd-memhog" {
		t.Errorf("memory-heavy container should rank second; got %q", resp.Containers[1].Name)
	}
	if resp.Containers[2].Name != "lerd-small" {
		t.Errorf("the small container should rank last; got %q", resp.Containers[2].Name)
	}
	if resp.TotalCPUPercent < 5.19 || resp.TotalCPUPercent > 5.21 {
		t.Errorf("total cpu = %v, want ~5.2", resp.TotalCPUPercent)
	}
	if resp.TotalMemBytes != 530_000_000 {
		t.Errorf("total mem = %d", resp.TotalMemBytes)
	}
	if resp.HostMemBytes != 33_000_000_000 {
		t.Errorf("host mem = %d", resp.HostMemBytes)
	}
}

// secondCycleStats must take each container's second sample (the instantaneous
// CPU rate) and discard the first (podman's lifetime-average artifact), so the
// dashboard shows current load rather than a long-idle container reading busy.
func TestSecondCycleStats_PrefersSecondSample(t *testing.T) {
	lines := []string{
		// cycle 1: cumulative/lifetime CPU (the misleading first sample)
		"lerd-php84-fpm|0.80|62MB / 33GB|0.18%",
		"lerd-mysql|0.09|468MB / 33GB|1.40%",
		// cycle 2: instantaneous rate
		"lerd-php84-fpm|0.00|62MB / 33GB|0.18%",
		"lerd-mysql|0.05|468MB / 33GB|1.40%",
		// cycle 3 (partial): ignored, second sample already captured
		"lerd-php84-fpm|0.99|62MB / 33GB|0.18%",
	}
	rows := secondCycleStats(lines)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Name != "lerd-php84-fpm" || rows[1].Name != "lerd-mysql" {
		t.Fatalf("order not preserved: %v", []string{rows[0].Name, rows[1].Name})
	}
	if rows[0].CPUPercent != 0.00 {
		t.Errorf("php84-fpm cpu = %v, want 0.00 (second sample, not the 0.80 lifetime average)", rows[0].CPUPercent)
	}
	if rows[1].CPUPercent != 0.05 {
		t.Errorf("mysql cpu = %v, want 0.05 (second sample)", rows[1].CPUPercent)
	}
}

// When the stream is cut short before a second sample arrives, fall back to the
// first so the container is still reported rather than vanishing.
func TestSecondCycleStats_FallsBackToFirstWhenStreamCutShort(t *testing.T) {
	rows := secondCycleStats([]string{"lerd-redis|0.20|17MB / 33GB|0.05%"})
	if len(rows) != 1 || rows[0].Name != "lerd-redis" {
		t.Fatalf("rows = %v", rows)
	}
	if rows[0].CPUPercent != 0.20 {
		t.Errorf("cpu = %v, want 0.20 fallback", rows[0].CPUPercent)
	}
}

func TestRead_HandlesNoContainers(t *testing.T) {
	noHostProcesses(t)
	restore := SetReader(func() ([]ContainerStat, error) { return nil, nil })
	t.Cleanup(restore)

	resp := Read()
	if resp.Available {
		t.Errorf("expected Available=false for empty container list")
	}
	if len(resp.Containers) != 0 {
		t.Errorf("expected empty containers, got %d", len(resp.Containers))
	}
}

// Read merges lerd's host-side processes with the containers into one list and
// one set of totals, dropping any host unit that is really a container quadlet so
// it isn't double-counted.
func TestRead_MergesHostProcesses(t *testing.T) {
	t.Cleanup(SetReader(func() ([]ContainerStat, error) {
		return []ContainerStat{
			{Name: "lerd-mysql", CPUPercent: 0.1, MemBytes: 400_000_000, MemLimit: 33_000_000_000},
		}, nil
	}))
	t.Cleanup(SetHostReader(func() ([]ContainerStat, error) {
		return []ContainerStat{
			{Name: "lerd-ui", CPUPercent: 0.2, MemBytes: 66_000_000, MemLimit: 33_000_000_000},
			{Name: "lerd-vite-app", CPUPercent: 3.0, MemBytes: 180_000_000, MemLimit: 33_000_000_000},
			// A container quadlet unit also reported by the host reader: must be
			// dropped in favour of the podman row, not counted twice.
			{Name: "lerd-mysql", CPUPercent: 9.9, MemBytes: 400_000_000, MemLimit: 33_000_000_000},
		}, nil
	}))

	resp := Read()
	if !resp.Available {
		t.Fatal("expected Available=true")
	}
	if len(resp.Containers) != 3 {
		t.Fatalf("got %d rows, want 3 (mysql + ui + vite, mysql dup dropped)", len(resp.Containers))
	}
	// The host-side Vite dev server (highest combined load) should rank first.
	if resp.Containers[0].Name != "lerd-vite-app" {
		t.Errorf("first by combined load = %q, want lerd-vite-app", resp.Containers[0].Name)
	}
	// Totals span both sources, and the mysql duplicate is counted once.
	if resp.TotalMemBytes != 646_000_000 {
		t.Errorf("total mem = %d, want 646000000 (400+66+180)", resp.TotalMemBytes)
	}
	wantCPU := 0.1 + 0.2 + 3.0
	if resp.TotalCPUPercent < wantCPU-0.001 || resp.TotalCPUPercent > wantCPU+0.001 {
		t.Errorf("total cpu = %v, want ~%v", resp.TotalCPUPercent, wantCPU)
	}
}

func TestCached_SingleflightUnderConcurrentLoad(t *testing.T) {
	// Three goroutines hit Cached at the same instant after the value is
	// stale. The reader should be invoked exactly once; the other two
	// goroutines should wait on the inflight signal and see the result.
	noHostProcesses(t)
	var calls int64
	restore := SetReader(func() ([]ContainerStat, error) {
		atomic.AddInt64(&calls, 1)
		// Slow enough that the racing goroutines all enter Cached
		// before this one returns, exercising the inflight path.
		time.Sleep(50 * time.Millisecond)
		return []ContainerStat{{Name: "lerd-x", CPUPercent: 1}}, nil
	})
	t.Cleanup(restore)

	// Force the cache to be stale on entry.
	defaultCache.mu.Lock()
	defaultCache.value = nil
	defaultCache.at = time.Time{}
	defaultCache.inflight = nil
	defaultCache.mu.Unlock()

	const concurrency = 5
	results := make(chan Snapshot, concurrency)
	for i := 0; i < concurrency; i++ {
		go func() { results <- Cached(time.Hour) }()
	}
	for i := 0; i < concurrency; i++ {
		snap := <-results
		if !snap.Available || len(snap.Containers) != 1 {
			t.Errorf("call %d returned empty snapshot", i)
		}
	}
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Errorf("expected exactly 1 Reader call under singleflight, got %d", got)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{500, "500B"},
		{2048, "2KB"},
		{int64(5 * 1024 * 1024), "5MB"},
		{int64(2.5 * 1024 * 1024 * 1024), "2.5GB"},
	}
	for _, c := range cases {
		if got := FormatBytes(c.in); got != c.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
