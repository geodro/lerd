package registry

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// withStubHTTP installs a temporary RoundTripper that routes all requests
// through srv (so the production hostnames hub.docker.com / ghcr.io point at
// the test server). Cleanup restores the global httpClient.
func withStubHTTP(t *testing.T, srv *httptest.Server) {
	t.Helper()
	prev := httpClient
	httpClient = &http.Client{
		Transport: &rewriteHostTransport{base: srv.URL, inner: srv.Client().Transport},
		Timeout:   5 * time.Second,
	}
	t.Cleanup(func() { httpClient = prev })
}

type rewriteHostTransport struct {
	base  string
	inner http.RoundTripper
}

func (r *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Route any host through the stub server, preserving the path.
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(r.base, "http://")
	if r.inner != nil {
		return r.inner.RoundTrip(req)
	}
	return http.DefaultTransport.RoundTrip(req)
}

func withTempCacheDir(t *testing.T) {
	t.Helper()
	t.Setenv("LERD_REGISTRY_CACHE_DIR", t.TempDir())
}

func TestListTags_DockerHub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v2/repositories/library/mysql/tags/") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [
				{"name": "8.0", "last_updated": "2026-01-01T00:00:00Z", "images": [{"digest": "sha256:abc"}]},
				{"name": "8.4.3", "last_updated": "2026-04-01T00:00:00Z", "images": [{"digest": "sha256:def"}]},
				{"name": "9.1.0", "last_updated": "2026-04-15T00:00:00Z", "images": [{"digest": "sha256:fff"}]}
			]
		}`))
	}))
	defer srv.Close()
	withStubHTTP(t, srv)
	withTempCacheDir(t)

	tags, err := ListTags("docker.io/library/mysql:8.0")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tags))
	}
	if tags[0].Name != "8.0" || tags[1].Name != "8.4.3" || tags[2].Name != "9.1.0" {
		t.Errorf("unexpected tags: %+v", tags)
	}
}

func TestMaybeNewerTag_MinorMysql(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [
				{"name": "8.0", "last_updated": "2026-01-01T00:00:00Z"},
				{"name": "8.4.3", "last_updated": "2026-04-01T00:00:00Z"},
				{"name": "9.1.0", "last_updated": "2026-04-15T00:00:00Z"},
				{"name": "5.7", "last_updated": "2025-01-01T00:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()
	withStubHTTP(t, srv)
	withTempCacheDir(t)

	got, err := MaybeNewerTag("docker.io/library/mysql:8.0", StrategyMinor)
	if err != nil {
		t.Fatalf("MaybeNewerTag: %v", err)
	}
	if got == nil {
		t.Fatal("expected an upgrade recommendation")
	}
	if got.Name != "8.4.3" {
		t.Errorf("MaybeNewerTag = %q, want 8.4.3 (newest 8.x)", got.Name)
	}
}

func TestMaybeNewerTag_OfflineSilent(t *testing.T) {
	withTempCacheDir(t)
	prev := httpClient
	httpClient = &http.Client{
		Transport: errTransport{},
		Timeout:   1 * time.Second,
	}
	t.Cleanup(func() { httpClient = prev })

	got, err := MaybeNewerTag("docker.io/library/mysql:8.0", StrategyMinor)
	if err != nil {
		t.Errorf("offline must yield (nil, nil), got error: %v", err)
	}
	if got != nil {
		t.Errorf("offline must yield (nil, nil), got recommendation: %+v", got)
	}
}

type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errNetwork
}

var errNetwork = errors.New("simulated network failure")

func TestMaybeNewerTag_UnsupportedRegistry(t *testing.T) {
	withTempCacheDir(t)
	got, err := MaybeNewerTag("quay.io/example/whatever:1.0", StrategyMinor)
	if err != nil {
		t.Errorf("unsupported registry must yield (nil, nil), got error: %v", err)
	}
	if got != nil {
		t.Errorf("unsupported registry must yield (nil, nil), got: %+v", got)
	}
}

func TestNewestStable_RejectsMajorBump(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [
				{"name": "8.4", "last_updated": "2026-04-01T00:00:00Z"},
				{"name": "8.4.3", "last_updated": "2026-04-15T00:00:00Z"},
				{"name": "9.7", "last_updated": "2026-04-20T00:00:00Z"},
				{"name": "9.7.1", "last_updated": "2026-04-21T00:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()
	withStubHTTP(t, srv)
	withTempCacheDir(t)

	got, err := NewestStable("docker.io/library/mysql:8.4", false)
	if err != nil {
		t.Fatalf("NewestStable: %v", err)
	}
	if got == nil {
		t.Fatal("expected newest 8.x candidate, got nil")
	}
	if got.Name != "8.4.3" {
		t.Errorf("NewestStable picked %q, want 8.4.3 (must stay within major 8)", got.Name)
	}
}

func TestNewestStable_AllowMajorUpgrade(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [
				{"name": "8.4.3", "last_updated": "2026-04-15T00:00:00Z"},
				{"name": "9.7.1", "last_updated": "2026-04-21T00:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()
	withStubHTTP(t, srv)
	withTempCacheDir(t)

	got, err := NewestStable("docker.io/library/mysql:8.4", true)
	if err != nil {
		t.Fatalf("NewestStable: %v", err)
	}
	if got == nil || got.Name != "9.7.1" {
		t.Errorf("with allowMajorUpgrade=true, expected 9.7.1; got %v", got)
	}
}

func TestNewestStable_MeilisearchPatchToLatestMinor(t *testing.T) {
	// Meilisearch versions every minor in the 1.x line as a data-incompatible
	// jump, but they still share major=1. NewestStable should still surface
	// 1.42.x to a user on 1.7 so the UI can render an opt-in upgrade button.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"results": [
				{"name": "v1.7", "last_updated": "2024-01-01T00:00:00Z"},
				{"name": "v1.7.6", "last_updated": "2024-06-01T00:00:00Z"},
				{"name": "v1.42", "last_updated": "2026-04-01T00:00:00Z"},
				{"name": "v1.42.1", "last_updated": "2026-04-15T00:00:00Z"},
				{"name": "v2.0", "last_updated": "2026-04-20T00:00:00Z"}
			]
		}`))
	}))
	defer srv.Close()
	withStubHTTP(t, srv)
	withTempCacheDir(t)

	got, err := NewestStable("docker.io/getmeili/meilisearch:v1.7", false)
	if err != nil {
		t.Fatalf("NewestStable: %v", err)
	}
	if got == nil {
		t.Fatal("expected a 1.x candidate, got nil")
	}
	if got.Name != "v1.42.1" {
		t.Errorf("NewestStable picked %q, want v1.42.1 (newest within major 1)", got.Name)
	}
}

func TestListTags_CacheTTL(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results": [{"name": "1.0", "last_updated": "2026-01-01T00:00:00Z"}]}`))
	}))
	defer srv.Close()
	withStubHTTP(t, srv)
	withTempCacheDir(t)

	if _, err := ListTags("docker.io/library/redis:7"); err != nil {
		t.Fatalf("first ListTags: %v", err)
	}
	if _, err := ListTags("docker.io/library/redis:7"); err != nil {
		t.Fatalf("second ListTags: %v", err)
	}
	if hits != 1 {
		t.Errorf("expected exactly one HTTP hit (cache should serve the second), got %d", hits)
	}
}
