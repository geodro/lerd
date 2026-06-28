package cli

import (
	"errors"
	"testing"
)

// startInstalledFPM is the fast path ensureFPMRunning and ensureFPMStarted share:
// it reports the container as handled when it is already up or installed-but-
// stopped (starting it), and not-handled when the version isn't installed so the
// caller can prompt. Keeping it shared is what removed the duplicated sequence.
func TestStartInstalledFPM(t *testing.T) {
	origRunning, origInstalled, origStart := fpmContainerRunning, fpmIsInstalled, fpmStart
	t.Cleanup(func() {
		fpmContainerRunning, fpmIsInstalled, fpmStart = origRunning, origInstalled, origStart
	})

	// Already running: handled, nothing started.
	started := false
	fpmContainerRunning = func(string) (bool, error) { return true, nil }
	fpmIsInstalled = func(string, string) bool { return false }
	fpmStart = func(string, string) error { started = true; return nil }
	if handled, err := startInstalledFPM("8.4", "lerd-php84-fpm"); !handled || err != nil || started {
		t.Errorf("running container: handled=%v err=%v started=%v, want true/nil/false", handled, err, started)
	}

	// Installed but stopped: handled, started once, start error surfaced.
	started = false
	fpmContainerRunning = func(string) (bool, error) { return false, nil }
	fpmIsInstalled = func(string, string) bool { return true }
	fpmStart = func(string, string) error { started = true; return nil }
	if handled, err := startInstalledFPM("8.4", "lerd-php84-fpm"); !handled || err != nil || !started {
		t.Errorf("installed-stopped: handled=%v err=%v started=%v, want true/nil/true", handled, err, started)
	}
	wantErr := errors.New("boom")
	fpmStart = func(string, string) error { return wantErr }
	if handled, err := startInstalledFPM("8.4", "lerd-php84-fpm"); !handled || !errors.Is(err, wantErr) {
		t.Errorf("installed-stopped start failure: handled=%v err=%v, want true + boom", handled, err)
	}

	// Not installed: not handled so the caller prompts; nothing started.
	started = false
	fpmIsInstalled = func(string, string) bool { return false }
	if handled, err := startInstalledFPM("8.4", "lerd-php84-fpm"); handled || err != nil || started {
		t.Errorf("not-installed: handled=%v err=%v started=%v, want false/nil/false", handled, err, started)
	}
}
