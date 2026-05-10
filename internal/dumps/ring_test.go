package dumps

import (
	"fmt"
	"testing"
)

func mkEvent(id string) Event {
	return Event{V: 1, ID: id, Kind: KindDump, Ctx: Context{Type: "fpm", Site: "acme"}}
}

func TestRing_AppendUnderCap(t *testing.T) {
	r := NewRing(4)
	r.Append(mkEvent("a"))
	r.Append(mkEvent("b"))
	got := r.Snapshot()
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("snapshot = %v", ids(got))
	}
}

func TestRing_AppendWrapsAroundOldestEvicted(t *testing.T) {
	r := NewRing(3)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		r.Append(mkEvent(id))
	}
	got := r.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if want := []string{"c", "d", "e"}; !equalIDs(got, want) {
		t.Errorf("snapshot = %v, want %v", ids(got), want)
	}
}

func TestRing_SnapshotIsolation(t *testing.T) {
	r := NewRing(4)
	r.Append(mkEvent("a"))
	snap := r.Snapshot()
	snap[0].ID = "mutated"
	if r.Snapshot()[0].ID != "a" {
		t.Errorf("snapshot mutation leaked into ring")
	}
}

func TestRing_ClearResetsLen(t *testing.T) {
	r := NewRing(4)
	r.Append(mkEvent("a"))
	r.Clear()
	if r.Len() != 0 {
		t.Errorf("len after Clear = %d, want 0", r.Len())
	}
	if len(r.Snapshot()) != 0 {
		t.Errorf("snapshot after Clear non-empty")
	}
	r.Append(mkEvent("z"))
	if got := r.Snapshot(); len(got) != 1 || got[0].ID != "z" {
		t.Errorf("post-clear append = %v", ids(got))
	}
}

func TestRing_FilterBySite(t *testing.T) {
	r := NewRing(8)
	r.Append(Event{V: 1, ID: "a", Kind: KindDump, Ctx: Context{Type: "fpm", Site: "one"}})
	r.Append(Event{V: 1, ID: "b", Kind: KindDump, Ctx: Context{Type: "fpm", Site: "two"}})
	r.Append(Event{V: 1, ID: "c", Kind: KindDump, Ctx: Context{Type: "cli", Site: "one"}})
	got := r.Filter(FilterOpts{Site: "one"})
	if !equalIDs(got, []string{"a", "c"}) {
		t.Errorf("filter site one = %v", ids(got))
	}
}

func TestRing_FilterByCtx(t *testing.T) {
	r := NewRing(8)
	r.Append(Event{V: 1, ID: "a", Kind: KindDump, Ctx: Context{Type: "fpm"}})
	r.Append(Event{V: 1, ID: "b", Kind: KindDump, Ctx: Context{Type: "cli"}})
	got := r.Filter(FilterOpts{Ctx: "cli"})
	if !equalIDs(got, []string{"b"}) {
		t.Errorf("filter ctx cli = %v", ids(got))
	}
}

func TestRing_FilterSinceID(t *testing.T) {
	r := NewRing(8)
	for _, id := range []string{"a", "b", "c", "d"} {
		r.Append(mkEvent(id))
	}
	got := r.Filter(FilterOpts{SinceID: "b"})
	if !equalIDs(got, []string{"c", "d"}) {
		t.Errorf("filter since b = %v", ids(got))
	}
}

func TestRing_FilterLimitKeepsMostRecent(t *testing.T) {
	r := NewRing(8)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		r.Append(mkEvent(id))
	}
	got := r.Filter(FilterOpts{Limit: 2})
	if !equalIDs(got, []string{"d", "e"}) {
		t.Errorf("filter limit 2 = %v", ids(got))
	}
}

func TestRing_DefaultCapWhenZero(t *testing.T) {
	r := NewRing(0)
	if r.Cap() != DefaultCapacity {
		t.Errorf("cap = %d, want %d", r.Cap(), DefaultCapacity)
	}
}

func TestRing_ConcurrentAppendDoesntPanic(t *testing.T) {
	r := NewRing(64)
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func(seed int) {
			for j := 0; j < 200; j++ {
				r.Append(mkEvent(fmt.Sprintf("%d-%d", seed, j)))
			}
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	if r.Len() != 64 {
		t.Errorf("len after concurrent append = %d, want 64 (saturated)", r.Len())
	}
}

func ids(es []Event) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

func equalIDs(es []Event, want []string) bool {
	if len(es) != len(want) {
		return false
	}
	for i, e := range es {
		if e.ID != want[i] {
			return false
		}
	}
	return true
}
