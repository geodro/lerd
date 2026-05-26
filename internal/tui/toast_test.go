package tui

import (
	"strings"
	"testing"
	"time"
)

func TestEnqueueToast_AppendsAndCapsAtMax(t *testing.T) {
	m := NewModel("test")
	for i := 0; i < maxVisibleToasts+2; i++ {
		m.enqueueToast(toastSuccess, "title"+string(rune('a'+i)), "")
	}
	if len(m.toasts) != maxVisibleToasts {
		t.Fatalf("expected at most %d toasts, got %d", maxVisibleToasts, len(m.toasts))
	}
	// Oldest two should have been evicted; first remaining is index 2.
	if m.toasts[0].title != "title"+string(rune('a'+2)) {
		t.Errorf("oldest survivor wrong: %q", m.toasts[0].title)
	}
}

func TestEnqueueToast_CoalescesDuplicates(t *testing.T) {
	m := NewModel("test")
	m.enqueueToast(toastSuccess, "redis up", "")
	first := m.toasts[0].ts
	time.Sleep(2 * time.Millisecond)
	m.enqueueToast(toastSuccess, "redis up", "")
	if len(m.toasts) != 1 {
		t.Fatalf("expected coalesce to single entry, got %d", len(m.toasts))
	}
	if !m.toasts[0].ts.After(first) {
		t.Error("duplicate enqueue should refresh the timestamp")
	}
}

func TestPruneExpiredToasts_DropsOldEntries(t *testing.T) {
	m := NewModel("test")
	m.toasts = []toast{
		{kind: toastSuccess, title: "old", ts: time.Now().Add(-2 * toastTTLSuccess)},
		{kind: toastSuccess, title: "fresh", ts: time.Now()},
	}
	m.pruneExpiredToasts()
	if len(m.toasts) != 1 || m.toasts[0].title != "fresh" {
		t.Errorf("prune left wrong set: %+v", m.toasts)
	}
}

func TestPruneExpiredToasts_HonoursPerKindTTL(t *testing.T) {
	m := NewModel("test")
	// Crafted so a success in this window expires but a failure does not:
	// 5s ago is past the 3s success TTL but well before the 10s fail TTL.
	m.toasts = []toast{
		{kind: toastSuccess, title: "old-success", ts: time.Now().Add(-5 * time.Second)},
		{kind: toastFail, title: "lingering-error", ts: time.Now().Add(-5 * time.Second)},
	}
	m.pruneExpiredToasts()
	if len(m.toasts) != 1 || m.toasts[0].title != "lingering-error" {
		t.Errorf("expected the fail toast to outlive the success: %+v", m.toasts)
	}
}

func TestDismissNewestToast_PopsLast(t *testing.T) {
	m := NewModel("test")
	m.enqueueToast(toastSuccess, "a", "")
	m.enqueueToast(toastSuccess, "b", "")
	m.dismissNewestToast()
	if len(m.toasts) != 1 || m.toasts[0].title != "a" {
		t.Errorf("dismiss should pop newest; got %+v", m.toasts)
	}
}

func TestEnqueueToastForResult_MapsToKind(t *testing.T) {
	m := NewModel("test")
	m.enqueueToastForResult(ActionResult{Summary: "lerd status"})
	if m.toasts[0].kind != toastSuccess {
		t.Errorf("success result should produce success toast, got kind=%d", m.toasts[0].kind)
	}
}

func TestRenderToasts_RightAlignsWhenActive(t *testing.T) {
	m := NewModel("test")
	m.enqueueToast(toastSuccess, "all healthy", "")
	got := stripANSI(m.renderToasts(120))
	if !strings.Contains(got, "all healthy") {
		t.Errorf("expected toast title in output:\n%s", got)
	}
	// PlaceHorizontal pads the left with spaces so the toast sits on
	// the right — first non-whitespace char should be far from column 0.
	firstLine := strings.SplitN(got, "\n", 2)[0]
	leadSpaces := len(firstLine) - len(strings.TrimLeft(firstLine, " "))
	if leadSpaces < 20 {
		t.Errorf("expected toast to be right-aligned (leading spaces ~half of width), got %d leading spaces in %q", leadSpaces, firstLine)
	}
}

func TestRenderToasts_EmptyWhenNoToasts(t *testing.T) {
	m := NewModel("test")
	if got := m.renderToasts(120); got != "" {
		t.Errorf("expected empty output with no toasts, got %q", got)
	}
}
