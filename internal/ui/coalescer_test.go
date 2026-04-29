package ui

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestPollPublisher_burstCoalescesToOne(t *testing.T) {
	var calls atomic.Int32
	p := newPollPublisher(40*time.Millisecond, func() { calls.Add(1) })

	for i := 0; i < 100; i++ {
		p.trigger()
	}

	time.Sleep(150 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (a burst should collapse)", got)
	}
}

func TestPollPublisher_triggersAfterFireRescheduleSeparately(t *testing.T) {
	var calls atomic.Int32
	p := newPollPublisher(20*time.Millisecond, func() { calls.Add(1) })

	p.trigger()
	time.Sleep(80 * time.Millisecond)

	p.trigger()
	time.Sleep(80 * time.Millisecond)

	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 (two separated triggers should both fire)", got)
	}
}

func TestPollPublisher_triggerWithinWindowExtendsDelay(t *testing.T) {
	var firedAt atomic.Int64
	start := time.Now()
	p := newPollPublisher(50*time.Millisecond, func() {
		firedAt.Store(time.Since(start).Milliseconds())
	})

	p.trigger()
	time.Sleep(30 * time.Millisecond) // within the 50ms window
	p.trigger()                       // resets timer; fire at ~80ms
	time.Sleep(150 * time.Millisecond)

	got := firedAt.Load()
	if got < 70 {
		t.Errorf("fired at %dms, expected >= 70ms (timer should reset on second trigger)", got)
	}
}
