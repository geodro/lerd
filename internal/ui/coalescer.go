package ui

import (
	"sync"
	"time"
)

// pollPublisher coalesces a burst of trigger() calls into a single delayed
// invocation of action. Every trigger restarts the quiet-period timer so the
// action runs once, ~delay after the last trigger in a burst.
//
// A burst of 30 systemd SubState transitions used to spawn 30 concurrent
// `podman ps` subprocesses; this collapses them to one.
type pollPublisher struct {
	mu     sync.Mutex
	timer  *time.Timer
	delay  time.Duration
	action func()
}

func newPollPublisher(delay time.Duration, action func()) *pollPublisher {
	return &pollPublisher{delay: delay, action: action}
}

func (p *pollPublisher) trigger() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer == nil {
		p.timer = time.AfterFunc(p.delay, p.action)
		return
	}
	p.timer.Reset(p.delay)
}
