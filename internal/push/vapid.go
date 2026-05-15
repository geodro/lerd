// Package push owns Web Push plumbing: per-install VAPID keys, browser
// subscription store, and the encrypted POST that wakes a service worker
// when the notifier dispatches a new event.
package push

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/geodro/lerd/internal/config"
)

const (
	vapidPrivateFile = "vapid-private.key"
	vapidPublicFile  = "vapid-public.key"
)

// VAPIDSubject identifies this app in the JWT "sub" claim. HTTPS URLs are
// accepted by every push service; mailto: with non-routable hosts isn't.
const VAPIDSubject = "https://github.com/geodro/lerd"

var (
	vapidMu     sync.Mutex
	cachedPriv  string
	cachedPub   string
	cacheLoaded bool
)

func privPath() string { return filepath.Join(config.DataDir(), vapidPrivateFile) }
func pubPath() string  { return filepath.Join(config.DataDir(), vapidPublicFile) }

// VAPIDKeys returns the per-install VAPID key pair, generating it on first
// call and persisting both halves under the data dir.
func VAPIDKeys() (priv, pub string, err error) {
	vapidMu.Lock()
	defer vapidMu.Unlock()
	if cacheLoaded {
		return cachedPriv, cachedPub, nil
	}

	if p, q, ok := readKeyPairFromDisk(); ok {
		cachedPriv, cachedPub = p, q
		cacheLoaded = true
		return p, q, nil
	}

	p, q, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", fmt.Errorf("generating VAPID keys: %w", err)
	}
	if err := os.MkdirAll(config.DataDir(), 0o755); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(privPath(), []byte(p+"\n"), 0o600); err != nil {
		return "", "", fmt.Errorf("writing VAPID private key: %w", err)
	}
	if err := os.WriteFile(pubPath(), []byte(q+"\n"), 0o644); err != nil {
		return "", "", fmt.Errorf("writing VAPID public key: %w", err)
	}
	cachedPriv, cachedPub = p, q
	cacheLoaded = true
	return p, q, nil
}

func VAPIDPublicKey() (string, error) {
	_, pub, err := VAPIDKeys()
	return pub, err
}

func readKeyPairFromDisk() (priv, pub string, ok bool) {
	pBytes, err := os.ReadFile(privPath())
	if err != nil {
		return "", "", false
	}
	qBytes, err := os.ReadFile(pubPath())
	if err != nil {
		return "", "", false
	}
	priv = strings.TrimSpace(string(pBytes))
	pub = strings.TrimSpace(string(qBytes))
	if priv == "" || pub == "" {
		return "", "", false
	}
	return priv, pub, true
}

func resetForTest() {
	vapidMu.Lock()
	defer vapidMu.Unlock()
	cachedPriv, cachedPub = "", ""
	cacheLoaded = false
}
