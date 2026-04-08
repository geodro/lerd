package cli

import (
	"testing"
	"time"
)

// setupRemoteSetupTokenDir points DataDir at a temp directory and clears any
// pre-existing token file. Mirrors the XDG_DATA_HOME trick used by
// install_test.go and park_test.go.
func setupRemoteSetupTokenDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("HOME", tmp)
	_ = ClearRemoteSetupToken()
}

func TestSaveAndLoadRemoteSetupToken(t *testing.T) {
	setupRemoteSetupTokenDir(t)

	// Empty initially.
	if got, err := LoadRemoteSetupToken(); err != nil {
		t.Fatalf("LoadRemoteSetupToken initial: %v", err)
	} else if got != nil {
		t.Errorf("expected nil token initially, got %+v", got)
	}

	// Round-trip a token.
	want := &RemoteSetupToken{
		Token:   "ABCD1234",
		Expires: time.Now().Add(15 * time.Minute).UTC().Round(time.Second),
	}
	if err := SaveRemoteSetupToken(want); err != nil {
		t.Fatalf("SaveRemoteSetupToken: %v", err)
	}
	got, err := LoadRemoteSetupToken()
	if err != nil {
		t.Fatalf("LoadRemoteSetupToken after save: %v", err)
	}
	if got == nil {
		t.Fatal("expected loaded token, got nil")
	}
	if got.Token != want.Token {
		t.Errorf("Token = %q, want %q", got.Token, want.Token)
	}
	if !got.Expires.Equal(want.Expires) {
		t.Errorf("Expires = %v, want %v", got.Expires, want.Expires)
	}
	if got.Failures != 0 {
		t.Errorf("Failures = %d, want 0", got.Failures)
	}
}

func TestClearRemoteSetupToken(t *testing.T) {
	setupRemoteSetupTokenDir(t)

	// Clearing a missing token is a no-op.
	if err := ClearRemoteSetupToken(); err != nil {
		t.Errorf("ClearRemoteSetupToken on empty: %v", err)
	}

	// Save then clear.
	if err := SaveRemoteSetupToken(&RemoteSetupToken{Token: "X", Expires: time.Now().Add(time.Minute)}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := ClearRemoteSetupToken(); err != nil {
		t.Fatalf("ClearRemoteSetupToken: %v", err)
	}
	if got, _ := LoadRemoteSetupToken(); got != nil {
		t.Errorf("expected token to be cleared, got %+v", got)
	}
}

func TestRecordRemoteSetupFailure_increments(t *testing.T) {
	setupRemoteSetupTokenDir(t)
	if err := SaveRemoteSetupToken(&RemoteSetupToken{
		Token:   "ABCD1234",
		Expires: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	for i := 1; i < MaxRemoteSetupFailures; i++ {
		closed, err := RecordRemoteSetupFailure()
		if err != nil {
			t.Fatalf("RecordRemoteSetupFailure %d: %v", i, err)
		}
		if closed {
			t.Errorf("attempt %d: closed early", i)
		}
		got, _ := LoadRemoteSetupToken()
		if got == nil {
			t.Fatalf("attempt %d: token unexpectedly cleared", i)
		}
		if got.Failures != i {
			t.Errorf("attempt %d: Failures = %d, want %d", i, got.Failures, i)
		}
	}
}

func TestRecordRemoteSetupFailure_closesAtLimit(t *testing.T) {
	setupRemoteSetupTokenDir(t)
	if err := SaveRemoteSetupToken(&RemoteSetupToken{
		Token:   "ABCD1234",
		Expires: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	var lastClosed bool
	for i := 0; i < MaxRemoteSetupFailures; i++ {
		closed, err := RecordRemoteSetupFailure()
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		lastClosed = closed
	}
	if !lastClosed {
		t.Error("expected the final attempt to return closed=true")
	}
	if got, _ := LoadRemoteSetupToken(); got != nil {
		t.Errorf("expected token to be cleared after %d failures, got %+v", MaxRemoteSetupFailures, got)
	}
}

func TestRecordRemoteSetupFailure_noToken(t *testing.T) {
	setupRemoteSetupTokenDir(t)
	closed, err := RecordRemoteSetupFailure()
	if err != nil {
		t.Errorf("RecordRemoteSetupFailure with no active token: %v", err)
	}
	if !closed {
		t.Error("expected closed=true when no token is active")
	}
}

func TestGenerateRemoteSetupCode_format(t *testing.T) {
	code, err := generateRemoteSetupCode()
	if err != nil {
		t.Fatalf("generateRemoteSetupCode: %v", err)
	}
	if len(code) != 8 {
		t.Errorf("code length = %d, want 8", len(code))
	}
	const allowed = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	for _, ch := range code {
		found := false
		for _, a := range allowed {
			if ch == a {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("code contains disallowed character %q", ch)
		}
	}
}
