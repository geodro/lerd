//go:build !nogui

package tray

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestOffOn_FlipsState(t *testing.T) {
	if got := offOn(true); got != "off" {
		t.Errorf("offOn(true) = %q, want %q", got, "off")
	}
	if got := offOn(false); got != "on" {
		t.Errorf("offOn(false) = %q, want %q", got, "on")
	}
}

func TestRunAndRefresh_CallsRefreshAfterCommand(t *testing.T) {
	called := false
	runAndRefresh(exec.Command("true"), func() { called = true })
	if !called {
		t.Error("refresh should be called after command exits")
	}
}

func TestRunAndRefresh_CallsRefreshEvenWhenCommandFails(t *testing.T) {
	called := false
	runAndRefresh(exec.Command("false"), func() { called = true })
	if !called {
		t.Error("refresh should be called even if the command fails so the menu still redraws")
	}
}

func TestLerdBin_FallbackWarnsOnceAndReturnsBareName(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")

	prevLook := lerdBinLookPath
	prevCands := lerdBinCandidates
	prevW := lerdBinWarnW
	lerdBinLookPath = func(string) (string, error) { return "", errors.New("not on PATH") }
	lerdBinCandidates = func() []string { return []string{missing} }
	var buf bytes.Buffer
	lerdBinWarnW = &buf
	lerdBinFallbackWarn = sync.Once{}
	t.Cleanup(func() {
		lerdBinLookPath = prevLook
		lerdBinCandidates = prevCands
		lerdBinWarnW = prevW
		lerdBinFallbackWarn = sync.Once{}
	})

	if got := lerdBin(); got != "lerd" {
		t.Errorf("first call: lerdBin() = %q, want %q (bare fallback)", got, "lerd")
	}
	if !strings.Contains(buf.String(), "could not locate the lerd binary") {
		t.Errorf("expected fallback warning on stderr, got: %q", buf.String())
	}
	first := buf.Len()

	if got := lerdBin(); got != "lerd" {
		t.Errorf("second call: lerdBin() = %q, want %q", got, "lerd")
	}
	if buf.Len() != first {
		t.Errorf("sync.Once should suppress repeat warnings; buffer grew from %d to %d", first, buf.Len())
	}
}

func TestLerdBin_PrefersLookPathHit(t *testing.T) {
	prevLook := lerdBinLookPath
	prevCands := lerdBinCandidates
	lerdBinLookPath = func(string) (string, error) { return "/opt/from-path/lerd", nil }
	lerdBinCandidates = func() []string {
		t.Error("candidates must not be consulted when LookPath succeeds")
		return nil
	}
	t.Cleanup(func() {
		lerdBinLookPath = prevLook
		lerdBinCandidates = prevCands
	})

	if got := lerdBin(); got != "/opt/from-path/lerd" {
		t.Errorf("lerdBin() = %q, want LookPath result", got)
	}
}
