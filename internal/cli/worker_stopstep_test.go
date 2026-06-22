package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/feedback"
)

// A non-fatal daemon-reload warning must be emitted after the step completes,
// not before, so the live spinner can't overwrite it. With output captured the
// symptom shows as line order: the step's completion line must precede the
// warning.
func TestFinalizeStopStepOrdersWarningAfterStep(t *testing.T) {
	var buf bytes.Buffer
	restore := feedback.SetTestWriter(&buf)
	defer restore()

	step := feedback.Start("stopping worker")
	finalizeStopStep(step, errors.New("boom"))

	out := buf.String()
	stepIdx := strings.Index(out, "stopping worker")
	warnIdx := strings.Index(out, "daemon-reload")
	if stepIdx < 0 || warnIdx < 0 {
		t.Fatalf("expected both the step line and the warning in output, got:\n%s", out)
	}
	if stepIdx > warnIdx {
		t.Fatalf("warning printed before the step completion line:\n%s", out)
	}
}

// No daemon-reload error means no warning at all.
func TestFinalizeStopStepNoWarningOnSuccess(t *testing.T) {
	var buf bytes.Buffer
	restore := feedback.SetTestWriter(&buf)
	defer restore()

	step := feedback.Start("stopping worker")
	finalizeStopStep(step, nil)

	if strings.Contains(buf.String(), "daemon-reload") {
		t.Fatalf("unexpected daemon-reload warning on success:\n%s", buf.String())
	}
}
