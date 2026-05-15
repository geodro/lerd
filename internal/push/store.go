package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/config"
)

const subsFile = "push-subscriptions.json"

// Subscription is the persisted per-browser push subscription. EnabledKinds
// gates push fan-out so a user can mute categories per device; an empty
// slice with Enabled=true means "all kinds" (default for new subs).
type Subscription struct {
	Endpoint     string   `json:"endpoint"`
	P256dh       string   `json:"p256dh"`
	Auth         string   `json:"auth"`
	UA           string   `json:"ua,omitempty"`
	AddedAt      int64    `json:"added_at"`
	Enabled      bool     `json:"enabled"`
	EnabledKinds []string `json:"enabled_kinds,omitempty"`
}

func (s Subscription) id() string { return s.Endpoint }

// Allows reports whether this subscription should receive a push for the
// given notification kind. "test" is always allowed so users can verify
// the pipeline even when no categories are toggled on.
func (s Subscription) Allows(kind string) bool {
	if !s.Enabled {
		return false
	}
	if kind == "test" {
		return true
	}
	if len(s.EnabledKinds) == 0 {
		return true
	}
	for _, k := range s.EnabledKinds {
		if k == kind {
			return true
		}
	}
	return false
}

var storeMu sync.Mutex

func subsPath() string { return filepath.Join(config.DataDir(), subsFile) }

func load() ([]Subscription, error) {
	data, err := os.ReadFile(subsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var out []Subscription
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", subsPath(), err)
	}
	return out, nil
}

func save(subs []Subscription) error {
	if err := os.MkdirAll(config.DataDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(subs, "", "  ")
	if err != nil {
		return err
	}
	tmp := subsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, subsPath())
}

func List() ([]Subscription, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	return load()
}

func Add(s Subscription) error {
	if s.Endpoint == "" || s.P256dh == "" || s.Auth == "" {
		return errors.New("subscription missing endpoint/p256dh/auth")
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	subs, err := load()
	if err != nil {
		return err
	}
	if s.AddedAt == 0 {
		s.AddedAt = time.Now().Unix()
	}
	updated := make([]Subscription, 0, len(subs)+1)
	for _, e := range subs {
		if e.id() == s.id() {
			continue
		}
		updated = append(updated, e)
	}
	updated = append(updated, s)
	return save(updated)
}

func Remove(endpoint string) error {
	if endpoint == "" {
		return nil
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	subs, err := load()
	if err != nil {
		return err
	}
	updated := make([]Subscription, 0, len(subs))
	for _, e := range subs {
		if e.id() == endpoint {
			continue
		}
		updated = append(updated, e)
	}
	if len(updated) == len(subs) {
		return nil
	}
	return save(updated)
}
