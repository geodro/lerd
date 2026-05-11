package dumps

import (
	"testing"
	"time"
)

func TestHub_SubscribeReceivesPublish(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	defer unsub()
	go h.Publish(mkEvent("a"))
	select {
	case e := <-ch:
		if e.ID != "a" {
			t.Errorf("got %q, want a", e.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for publish")
	}
}

func TestHub_UnsubscribeStopsDelivery(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	unsub()
	// channel should be closed
	if _, ok := <-ch; ok {
		t.Errorf("unsubscribed channel still open")
	}
	if h.Count() != 0 {
		t.Errorf("count after unsub = %d, want 0", h.Count())
	}
}

func TestHub_UnsubscribeIdempotent(t *testing.T) {
	h := NewHub()
	_, unsub := h.Subscribe()
	unsub()
	unsub() // must not double-close
}

func TestHub_SlowSubscriberDropsEvents(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe()
	defer unsub()
	for i := 0; i < subBuffer*2; i++ {
		h.Publish(mkEvent("x"))
	}
	// drain everything available without blocking
	got := 0
	for {
		select {
		case <-ch:
			got++
		default:
			if got == 0 {
				t.Fatal("subscriber received nothing")
			}
			if got > subBuffer {
				t.Errorf("got %d events, expected <= buffer (%d)", got, subBuffer)
			}
			return
		}
	}
}

func TestHub_MultipleSubscribersAllReceive(t *testing.T) {
	h := NewHub()
	ch1, u1 := h.Subscribe()
	ch2, u2 := h.Subscribe()
	defer u1()
	defer u2()
	go h.Publish(mkEvent("z"))
	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case e := <-ch:
			if e.ID != "z" {
				t.Errorf("got %q, want z", e.ID)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber timed out")
		}
	}
}
