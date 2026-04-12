package eventbus

import (
	"sort"
	"testing"
	"time"
)

func TestPublishDebouncesIntoOneBroadcast(t *testing.T) {
	h := New()
	h.SetDebounce(20 * time.Millisecond)
	sub := h.Subscribe()
	defer h.Unsubscribe(sub)

	h.Publish(KindSites)
	h.Publish(KindServices)
	h.Publish(KindSites)

	select {
	case evt := <-sub.C:
		sort.Strings(evt.Kinds)
		if len(evt.Kinds) != 2 || evt.Kinds[0] != KindServices || evt.Kinds[1] != KindSites {
			t.Fatalf("unexpected kinds: %v", evt.Kinds)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected one coalesced event")
	}

	select {
	case <-sub.C:
		t.Fatal("did not expect a second event")
	case <-time.After(60 * time.Millisecond):
	}
}

func TestSlowSubscriberDropped(t *testing.T) {
	h := New()
	h.SetDebounce(5 * time.Millisecond)
	sub := h.Subscribe()

	// Fill the buffer without draining, then publish one more round.
	for i := 0; i < 20; i++ {
		h.Publish(KindSites)
		time.Sleep(10 * time.Millisecond)
	}

	// The subscriber's channel must be closed eventually.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		_, stillThere := h.subs[sub]
		h.mu.Unlock()
		if !stillThere {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("slow subscriber was not dropped")
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	h := New()
	h.SetDebounce(5 * time.Millisecond)
	sub := h.Subscribe()
	h.Unsubscribe(sub)

	h.Publish(KindSites)
	select {
	case _, ok := <-sub.C:
		if ok {
			t.Fatal("did not expect an event after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMultipleSubscribersEachReceive(t *testing.T) {
	h := New()
	h.SetDebounce(5 * time.Millisecond)
	a := h.Subscribe()
	b := h.Subscribe()
	defer h.Unsubscribe(a)
	defer h.Unsubscribe(b)

	h.Publish(KindStatus)

	for i, sub := range []*Subscriber{a, b} {
		select {
		case evt := <-sub.C:
			if len(evt.Kinds) != 1 || evt.Kinds[0] != KindStatus {
				t.Fatalf("sub %d: unexpected kinds %v", i, evt.Kinds)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("sub %d: no event received", i)
		}
	}
}
