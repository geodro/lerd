package push

import (
	"os"
	"testing"
)

func TestStore_AddListRemove(t *testing.T) {
	withTempDataDir(t)

	got, err := List()
	if err != nil {
		t.Fatalf("List (empty): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty store, got %d entries", len(got))
	}

	sub := Subscription{
		Endpoint: "https://fcm.googleapis.com/fcm/send/aaa",
		P256dh:   "p256-key",
		Auth:     "auth-secret",
		UA:       "BraveTest",
	}
	if err := Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err = List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Endpoint != sub.Endpoint || got[0].P256dh != sub.P256dh || got[0].Auth != sub.Auth {
		t.Errorf("loaded entry doesn't match: %+v", got[0])
	}
	if got[0].AddedAt == 0 {
		t.Errorf("AddedAt was not stamped")
	}

	// Re-adding the same endpoint must dedupe (replace, not append).
	sub2 := sub
	sub2.P256dh = "rotated-key"
	if err := Add(sub2); err != nil {
		t.Fatalf("Add (replace): %v", err)
	}
	got, _ = List()
	if len(got) != 1 || got[0].P256dh != "rotated-key" {
		t.Errorf("dedup-on-add failed: %+v", got)
	}

	if err := Remove(sub.Endpoint); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, _ = List()
	if len(got) != 0 {
		t.Errorf("Remove left %d entries", len(got))
	}
}

func TestStore_AddRejectsIncompleteSubscription(t *testing.T) {
	withTempDataDir(t)

	cases := []Subscription{
		{Endpoint: "", P256dh: "p", Auth: "a"},
		{Endpoint: "https://x", P256dh: "", Auth: "a"},
		{Endpoint: "https://x", P256dh: "p", Auth: ""},
	}
	for i, c := range cases {
		if err := Add(c); err == nil {
			t.Errorf("case %d: expected error for %+v", i, c)
		}
	}
}

func TestStore_PersistedFileMode(t *testing.T) {
	dir := withTempDataDir(t)
	sub := Subscription{
		Endpoint: "https://example.org/x",
		P256dh:   "p",
		Auth:     "a",
	}
	if err := Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}
	info, err := os.Stat(dir + "/lerd/" + subsFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file perm = %o, want 0600", mode)
	}
}

func TestStore_RemoveMissingIsNoop(t *testing.T) {
	withTempDataDir(t)
	if err := Remove("https://nothing"); err != nil {
		t.Fatalf("Remove (missing) returned error: %v", err)
	}
}

func TestSubscription_Allows_MasterOffRejectsAll(t *testing.T) {
	s := Subscription{Enabled: false}
	for _, kind := range []string{"mail", "worker_failed", "op_done"} {
		if s.Allows(kind) {
			t.Errorf("Allows(%q) should be false when Enabled=false", kind)
		}
	}
}

func TestSubscription_Allows_EmptyKindsMeansAll(t *testing.T) {
	s := Subscription{Enabled: true}
	for _, kind := range []string{"mail", "worker_failed", "op_done", "dump"} {
		if !s.Allows(kind) {
			t.Errorf("Allows(%q) should be true for empty EnabledKinds + Enabled=true", kind)
		}
	}
}

func TestSubscription_Allows_RestrictsToEnabledKinds(t *testing.T) {
	s := Subscription{Enabled: true, EnabledKinds: []string{"mail", "worker_failed"}}
	if !s.Allows("mail") {
		t.Error("mail should be allowed")
	}
	if !s.Allows("worker_failed") {
		t.Error("worker_failed should be allowed")
	}
	if s.Allows("dump") {
		t.Error("dump should be blocked when not in EnabledKinds")
	}
}

func TestSubscription_Allows_TestAlwaysPasses(t *testing.T) {
	// The test push is how the user verifies the pipeline. It must fire
	// regardless of per-kind preferences as long as master Enabled=true.
	s := Subscription{Enabled: true, EnabledKinds: []string{"mail"}}
	if !s.Allows("test") {
		t.Error("test kind should bypass EnabledKinds gating")
	}
	s.Enabled = false
	if s.Allows("test") {
		t.Error("test still must respect master switch")
	}
}
