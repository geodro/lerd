package ui

import (
	"sync"
	"time"

	"github.com/geodro/lerd/internal/workerheal"
)

// workerFailureBatchDelay is the quiet window between the first new failure
// in a burst and the grouped dispatch. Five seconds collapses systemd
// cascades (start-limit storms, OOM kills hitting several queues at once)
// into one notification while still feeling immediate for a single failure.
var workerFailureBatchDelay = 5 * time.Second

// workerFailureDispatch is the sink used by the batcher. Production wires it
// to dispatchNotification; tests override it to observe the grouped payload
// without exercising the push subsystem.
var workerFailureDispatch = func(ws []workerheal.UnhealthyWorker) {
	dispatchNotification(notificationForWorkerFailures(ws))
}

var (
	workerFailureBatchMu    sync.Mutex
	pendingWorkerFailures   = map[string]workerheal.UnhealthyWorker{}
	workerFailureFlushTimer *time.Timer
)

// queueWorkerFailureNotifications buffers the watcher's new-failure delta
// and arms a single flush timer. The timer is intentionally not reset when
// more failures arrive inside the window, so the grouped push lands at most
// workerFailureBatchDelay after the *first* failure in the batch even if
// new failures keep trickling in.
func queueWorkerFailureNotifications(ws []workerheal.UnhealthyWorker) {
	if len(ws) == 0 {
		return
	}
	workerFailureBatchMu.Lock()
	for _, w := range ws {
		pendingWorkerFailures[w.Unit] = w
	}
	if workerFailureFlushTimer == nil {
		workerFailureFlushTimer = time.AfterFunc(workerFailureBatchDelay, flushPendingWorkerFailures)
	}
	workerFailureBatchMu.Unlock()
}

func flushPendingWorkerFailures() {
	workerFailureBatchMu.Lock()
	ws := make([]workerheal.UnhealthyWorker, 0, len(pendingWorkerFailures))
	for _, w := range pendingWorkerFailures {
		ws = append(ws, w)
	}
	pendingWorkerFailures = map[string]workerheal.UnhealthyWorker{}
	workerFailureFlushTimer = nil
	workerFailureBatchMu.Unlock()
	if len(ws) == 0 {
		return
	}
	workerFailureDispatch(ws)
}
