package ui

import (
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/workerheal"
)

// installBatchTestSink swaps the dispatcher and shrinks the delay so a real
// timer flush completes inside the test, then restores both on cleanup.
func installBatchTestSink(t *testing.T, delay time.Duration) (wait func() [][]workerheal.UnhealthyWorker) {
	t.Helper()
	var mu sync.Mutex
	var calls [][]workerheal.UnhealthyWorker
	done := make(chan struct{}, 8)

	origDispatch := workerFailureDispatch
	origDelay := workerFailureBatchDelay
	workerFailureDispatch = func(ws []workerheal.UnhealthyWorker) {
		mu.Lock()
		copyWs := append([]workerheal.UnhealthyWorker(nil), ws...)
		calls = append(calls, copyWs)
		mu.Unlock()
		done <- struct{}{}
	}
	workerFailureBatchDelay = delay
	t.Cleanup(func() {
		workerFailureDispatch = origDispatch
		workerFailureBatchDelay = origDelay
		workerFailureBatchMu.Lock()
		pendingWorkerFailures = map[string]workerheal.UnhealthyWorker{}
		if workerFailureFlushTimer != nil {
			workerFailureFlushTimer.Stop()
			workerFailureFlushTimer = nil
		}
		workerFailureBatchMu.Unlock()
	})
	return func() [][]workerheal.UnhealthyWorker {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for batched dispatch")
		}
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
}

func TestQueueWorkerFailureNotifications_BatchesWithinWindow(t *testing.T) {
	wait := installBatchTestSink(t, 200*time.Millisecond)
	queueWorkerFailureNotifications([]workerheal.UnhealthyWorker{
		uw("lerd-queue-a.service", "a.test", "queue", "failed"),
	})
	queueWorkerFailureNotifications([]workerheal.UnhealthyWorker{
		uw("lerd-horizon-a.service", "a.test", "horizon", "failed"),
		uw("lerd-scheduler-b.service", "b.test", "scheduler", "failed"),
	})
	calls := wait()
	if len(calls) != 1 {
		t.Fatalf("expected one grouped dispatch, got %d", len(calls))
	}
	got := calls[0]
	if len(got) != 3 {
		t.Fatalf("expected 3 batched workers, got %d", len(got))
	}
	units := make([]string, len(got))
	for i, w := range got {
		units[i] = w.Unit
	}
	sort.Strings(units)
	want := []string{
		"lerd-horizon-a.service",
		"lerd-queue-a.service",
		"lerd-scheduler-b.service",
	}
	for i := range want {
		if units[i] != want[i] {
			t.Errorf("units[%d] = %q, want %q", i, units[i], want[i])
		}
	}
}

func TestQueueWorkerFailureNotifications_DedupesByUnit(t *testing.T) {
	wait := installBatchTestSink(t, 200*time.Millisecond)
	queueWorkerFailureNotifications([]workerheal.UnhealthyWorker{
		uw("lerd-queue-a.service", "a.test", "queue", "failed"),
	})
	queueWorkerFailureNotifications([]workerheal.UnhealthyWorker{
		uw("lerd-queue-a.service", "a.test", "queue", "start-limit-hit"),
	})
	calls := wait()
	if len(calls) != 1 || len(calls[0]) != 1 {
		t.Fatalf("expected single dedup'd dispatch, got %+v", calls)
	}
	if calls[0][0].State != "start-limit-hit" {
		t.Errorf("expected last-write-wins state, got %q", calls[0][0].State)
	}
}

func TestQueueWorkerFailureNotifications_SecondBurstArmsFreshWindow(t *testing.T) {
	wait := installBatchTestSink(t, 100*time.Millisecond)
	queueWorkerFailureNotifications([]workerheal.UnhealthyWorker{
		uw("lerd-queue-a.service", "a.test", "queue", "failed"),
	})
	first := wait()
	if len(first) != 1 || len(first[0]) != 1 || first[0][0].Unit != "lerd-queue-a.service" {
		t.Fatalf("first burst dispatch malformed: %+v", first)
	}
	// After the first batch has fired and cleared state, a fresh failure
	// must arm a brand-new window and dispatch independently. Without this
	// the batcher would silently swallow all post-first-burst notifications.
	queueWorkerFailureNotifications([]workerheal.UnhealthyWorker{
		uw("lerd-horizon-b.service", "b.test", "horizon", "failed"),
	})
	second := wait()
	if len(second) != 2 {
		t.Fatalf("expected two dispatches across two bursts, got %d", len(second))
	}
	if len(second[1]) != 1 || second[1][0].Unit != "lerd-horizon-b.service" {
		t.Errorf("second burst payload wrong: %+v", second[1])
	}
}

func TestQueueWorkerFailureNotifications_EmptyNoDispatch(t *testing.T) {
	origDispatch := workerFailureDispatch
	defer func() { workerFailureDispatch = origDispatch }()
	var fired bool
	workerFailureDispatch = func([]workerheal.UnhealthyWorker) { fired = true }
	queueWorkerFailureNotifications(nil)
	queueWorkerFailureNotifications([]workerheal.UnhealthyWorker{})
	time.Sleep(20 * time.Millisecond)
	if fired {
		t.Errorf("empty batch should not arm timer or dispatch")
	}
}
