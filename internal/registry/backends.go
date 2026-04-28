package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// httpClient is overridable from tests; production uses a 10s-timeout client.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// cacheTTL is how long ListTags results stay valid on disk before re-fetching.
// 6 hours balances freshness against polling Docker Hub on every page load.
var cacheTTL = 6 * time.Hour

// cacheDir returns the on-disk path for cached registry responses, defaulting
// to ~/.cache/lerd/registry-tags. Override LERD_REGISTRY_CACHE_DIR for tests.
func cacheDir() string {
	if d := os.Getenv("LERD_REGISTRY_CACHE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return filepath.Join(d, "lerd", "registry-tags")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "lerd", "registry-tags")
}

// ListTags fetches available tags for the named image, choosing the backend
// by registry hostname. Returns *UnsupportedRegistryErr for unknown hosts and
// *UnreachableErr for network failures so callers can swallow the latter.
func ListTags(image string) ([]TagInfo, error) {
	ref, err := ParseImage(image)
	if err != nil {
		return nil, err
	}
	if cached, ok := readCache(ref); ok {
		return cached, nil
	}
	var tags []TagInfo
	switch ref.Registry {
	case "docker.io":
		tags, err = listTagsDockerHub(ref)
	case "ghcr.io":
		tags, err = listTagsGHCR(ref)
	default:
		return nil, &UnsupportedRegistryErr{Registry: ref.Registry}
	}
	if err != nil {
		return nil, err
	}
	writeCache(ref, tags)
	return tags, nil
}

// NewestStable returns the newest version-shaped tag in the registry,
// ignoring update_strategy. Same variant suffix is required. When
// allowMajorUpgrade is false the search stays within the current numeric
// major; when true any newer version in any major qualifies.
func NewestStable(image string, allowMajorUpgrade bool) (*TagInfo, error) {
	ref, err := ParseImage(image)
	if err != nil {
		return nil, err
	}
	tags, err := ListTags(image)
	if err != nil {
		var unreachable *UnreachableErr
		var unsupported *UnsupportedRegistryErr
		if errors.As(err, &unreachable) || errors.As(err, &unsupported) {
			return nil, nil
		}
		return nil, err
	}
	current := parseTag(ref.Tag)
	if len(current.Numeric) == 0 {
		return nil, nil
	}
	var best *TagInfo
	var bestNumeric []int
	for i := range tags {
		t := &tags[i]
		ct := parseTag(t.Name)
		if len(ct.Numeric) == 0 {
			continue
		}
		if !allowMajorUpgrade && ct.Numeric[0] != current.Numeric[0] {
			continue
		}
		if !sameVariant(current.Variant, ct.Variant) {
			continue
		}
		if !numericGreater(ct.Numeric, current.Numeric) {
			continue
		}
		if best == nil || numericGreater(ct.Numeric, bestNumeric) {
			best = t
			bestNumeric = ct.Numeric
		}
	}
	return best, nil
}

// MaybeNewerTag is the high-level entry point. It looks at the image's tag,
// queries the registry, applies the strategy, and returns either a newer tag
// to recommend or nil. Network errors are converted to (nil, nil) so the UI
// can stay quiet when the user is offline.
func MaybeNewerTag(image string, strategy Strategy) (*TagInfo, error) {
	if strategy == StrategyNone || strategy == "" {
		return nil, nil
	}
	ref, err := ParseImage(image)
	if err != nil {
		return nil, err
	}
	tags, err := ListTags(image)
	if err != nil {
		var unreachable *UnreachableErr
		var unsupported *UnsupportedRegistryErr
		if errors.As(err, &unreachable) || errors.As(err, &unsupported) {
			return nil, nil
		}
		return nil, err
	}
	current := parseTag(ref.Tag)
	return pickNewer(current, tags, strategy), nil
}

// ---- Docker Hub backend -----------------------------------------------------

type dockerHubResponse struct {
	Next    string             `json:"next"`
	Results []dockerHubTagItem `json:"results"`
}

type dockerHubTagItem struct {
	Name        string                  `json:"name"`
	LastUpdated time.Time               `json:"last_updated"`
	Digest      string                  `json:"digest,omitempty"`
	Images      []dockerHubImageVariant `json:"images,omitempty"`
}

type dockerHubImageVariant struct {
	Digest string `json:"digest"`
}

func listTagsDockerHub(ref ImageRef) ([]TagInfo, error) {
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags/?page_size=100&ordering=last_updated", ref.Repo)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &UnreachableErr{Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository not found: %s", ref.Repo)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &UnreachableErr{Cause: fmt.Errorf("docker hub rate-limited (429)")}
	}
	if resp.StatusCode >= 500 {
		return nil, &UnreachableErr{Cause: fmt.Errorf("docker hub %d", resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("docker hub %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed dockerHubResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, &UnreachableErr{Cause: err}
	}
	out := make([]TagInfo, 0, len(parsed.Results))
	for _, t := range parsed.Results {
		digest := t.Digest
		if digest == "" && len(t.Images) > 0 {
			digest = t.Images[0].Digest
		}
		out = append(out, TagInfo{
			Name:   t.Name,
			Pushed: t.LastUpdated,
			Digest: digest,
		})
	}
	return out, nil
}

// ---- GHCR backend -----------------------------------------------------------

type ghcrTagsResponse struct {
	Tags []string `json:"tags"`
}

type ghcrTokenResponse struct {
	Token string `json:"token"`
}

func listTagsGHCR(ref ImageRef) ([]TagInfo, error) {
	tokenURL := fmt.Sprintf("https://ghcr.io/token?scope=repository:%s:pull&service=ghcr.io", ref.Repo)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tokenReq, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	if err != nil {
		return nil, err
	}
	tokenResp, err := httpClient.Do(tokenReq)
	if err != nil {
		return nil, &UnreachableErr{Cause: err}
	}
	defer tokenResp.Body.Close()
	if tokenResp.StatusCode != http.StatusOK {
		return nil, &UnreachableErr{Cause: fmt.Errorf("ghcr token %d", tokenResp.StatusCode)}
	}
	var tok ghcrTokenResponse
	if err := json.NewDecoder(tokenResp.Body).Decode(&tok); err != nil {
		return nil, &UnreachableErr{Cause: err}
	}
	listURL := fmt.Sprintf("https://ghcr.io/v2/%s/tags/list?n=100", ref.Repo)
	req, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, &UnreachableErr{Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &UnreachableErr{Cause: fmt.Errorf("ghcr tags %d", resp.StatusCode)}
	}
	var parsed ghcrTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, &UnreachableErr{Cause: err}
	}
	out := make([]TagInfo, 0, len(parsed.Tags))
	for _, name := range parsed.Tags {
		out = append(out, TagInfo{Name: name})
	}
	return out, nil
}

// ---- Cache ------------------------------------------------------------------

type cachedEntry struct {
	FetchedAt time.Time `json:"fetched_at"`
	Tags      []TagInfo `json:"tags"`
}

func cacheKey(ref ImageRef) string {
	h := sha256.Sum256([]byte(ref.Registry + "/" + ref.Repo))
	return hex.EncodeToString(h[:])
}

func cachePath(ref ImageRef) string {
	return filepath.Join(cacheDir(), cacheKey(ref)+".json")
}

func readCache(ref ImageRef) ([]TagInfo, bool) {
	data, err := os.ReadFile(cachePath(ref))
	if err != nil {
		return nil, false
	}
	var entry cachedEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}
	if time.Since(entry.FetchedAt) > cacheTTL {
		return nil, false
	}
	return entry.Tags, true
}

func writeCache(ref ImageRef, tags []TagInfo) {
	if err := os.MkdirAll(cacheDir(), 0755); err != nil {
		return
	}
	entry := cachedEntry{FetchedAt: time.Now(), Tags: tags}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(cachePath(ref), data, 0644)
}
