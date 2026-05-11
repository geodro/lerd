package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/store"
)

// TestUpdateAllFrameworks_refreshesEveryCachedVersion pins the bug fixed by
// using info.Version instead of entry.Latest. With three versions cached, all
// three must be rewritten — not just the latest.
func TestUpdateAllFrameworks_refreshesEveryCachedVersion(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	storeDir := config.StoreFrameworksDir()
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Seed three stale yamls. Version is what makes ListFrameworksDetailed
	// surface them as separate entries and what FetchFramework will request.
	stale := func(version string) string {
		return "name: laravel\nlabel: Laravel\nversion: \"" + version + "\"\nconsole: artisan\nworkers:\n  queue:\n    command: stale\n"
	}
	for _, v := range []string{"10", "11", "12"} {
		path := filepath.Join(storeDir, "laravel@"+v+".yaml")
		if err := os.WriteFile(path, []byte(stale(v)), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Server returns a per-version yaml whose body embeds the version string
	// so we can assert each file was refetched at its own version.
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, _ *http.Request) {
		data, _ := json.Marshal(store.Index{
			Frameworks: []store.IndexEntry{{
				Name:     "laravel",
				Label:    "Laravel",
				Versions: []string{"12", "11", "10"},
				Latest:   "12",
			}},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})
	for _, v := range []string{"10", "11", "12"} {
		v := v
		mux.HandleFunc("/laravel/"+v+".yaml", func(w http.ResponseWriter, _ *http.Request) {
			body := "name: laravel\nlabel: Laravel\nversion: \"" + v + "\"\nconsole: artisan\nworkers:\n  queue:\n    command: fresh-v" + v + "\n"
			_, _ = w.Write([]byte(body))
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &store.Client{BaseURL: srv.URL}
	if err := updateAllFrameworks(client, false); err != nil {
		t.Fatalf("updateAllFrameworks: %v", err)
	}

	for _, v := range []string{"10", "11", "12"} {
		path := filepath.Join(storeDir, "laravel@"+v+".yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(data)
		want := "fresh-v" + v
		if !strings.Contains(body, want) {
			t.Errorf("laravel@%s.yaml was not refreshed; want substring %q, got:\n%s", v, want, body)
		}
		if strings.Contains(body, "stale") {
			t.Errorf("laravel@%s.yaml still contains stale marker", v)
		}
	}
}
