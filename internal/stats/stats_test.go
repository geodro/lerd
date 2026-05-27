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

func TestRead_SortsByMemDesc(t *testing.T) {
	restore := SetReader(func() ([]ContainerStat, error) {
		return []ContainerStat{
			{Name: "lerd-redis", CPUPercent: 0.5, MemBytes: 5_000_000, MemLimit: 33_000_000_000, MemPercent: 0.02},
			{Name: "lerd-mysql", CPUPercent: 1.5, MemBytes: 80_000_000, MemLimit: 33_000_000_000, MemPercent: 0.24},
			{Name: "lerd-postgres", CPUPercent: 0.1, MemBytes: 30_000_000, MemLimit: 33_000_000_000, MemPercent: 0.09},
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
	if resp.Containers[0].Name != "lerd-mysql" {
		t.Errorf("first should be biggest mem; got %q", resp.Containers[0].Name)
	}
	if resp.TotalCPUPercent != 2.1 {
		t.Errorf("total cpu = %v", resp.TotalCPUPercent)
	}
	if resp.TotalMemBytes != 115_000_000 {
		t.Errorf("total mem = %d", resp.TotalMemBytes)
	}
	if resp.HostMemBytes != 33_000_000_000 {
		t.Errorf("host mem = %d", resp.HostMemBytes)
	}
}

func TestRead_HandlesNoContainers(t *testing.T) {
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

func TestCached_SingleflightUnderConcurrentLoad(t *testing.T) {
	// Three goroutines hit Cached at the same instant after the value is
	// stale. The reader should be invoked exactly once; the other two
	// goroutines should wait on the inflight signal and see the result.
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
