package store

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	defaultBaseURL = "https://raw.githubusercontent.com/geodro/lerd-frameworks/main/frameworks"
	indexTTL       = 24 * time.Hour
	httpTimeout    = 10 * time.Second
)

// Client fetches framework definitions from the remote store.
type Client struct {
	BaseURL  string
	CacheDir string
}

// Index is the top-level store index listing all available frameworks.
type Index struct {
	Frameworks []IndexEntry `json:"frameworks"`
}

// IndexEntry describes a single framework available in the store.
type IndexEntry struct {
	Name     string                 `json:"name"`
	Label    string                 `json:"label"`
	Versions []string               `json:"versions"`
	Latest   string                 `json:"latest"`
	Detect   []config.FrameworkRule `json:"detect"`
}

// NewClient returns a store client with default settings.
func NewClient() *Client {
	return &Client{
		BaseURL:  defaultBaseURL,
		CacheDir: config.StoreCacheDir(),
	}
}

// FetchIndex downloads the store index, using a 24-hour disk cache.
func (c *Client) FetchIndex() (*Index, error) {
	cacheFile := filepath.Join(c.CacheDir, "index.json")

	// Check cache
	if data, err := os.ReadFile(cacheFile); err == nil {
		if info, statErr := os.Stat(cacheFile); statErr == nil && time.Since(info.ModTime()) < indexTTL {
			var idx Index
			if json.Unmarshal(data, &idx) == nil {
				return &idx, nil
			}
		}
	}

	// Fetch from remote
	data, err := c.fetch("index.json")
	if err != nil {
		return nil, fmt.Errorf("fetching store index: %w", err)
	}

	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing store index: %w", err)
	}

	// Write to cache
	if mkErr := os.MkdirAll(filepath.Dir(cacheFile), 0o755); mkErr == nil {
		os.WriteFile(cacheFile, data, 0o644) //nolint:errcheck
	}

	return &idx, nil
}

// FetchFramework downloads a framework definition from the store.
// Versioned definitions are cached indefinitely (they are immutable).
func (c *Client) FetchFramework(name, version string) (*config.Framework, error) {
	if version == "" {
		// Resolve latest from index
		idx, err := c.FetchIndex()
		if err != nil {
			return nil, err
		}
		entry, ok := c.findEntry(idx, name)
		if !ok {
			return nil, fmt.Errorf("framework %q not found in store", name)
		}
		version = entry.Latest
	}

	cacheFile := filepath.Join(c.CacheDir, name, version+".yaml")

	// Versioned definitions are immutable — cache hit means done
	if data, err := os.ReadFile(cacheFile); err == nil {
		var fw config.Framework
		if yaml.Unmarshal(data, &fw) == nil && fw.Name != "" {
			return &fw, nil
		}
	}

	// Fetch from remote
	remotePath := name + "/" + version + ".yaml"
	data, err := c.fetch(remotePath)
	if err != nil {
		return nil, fmt.Errorf("fetching %s@%s: %w", name, version, err)
	}

	var fw config.Framework
	if err := yaml.Unmarshal(data, &fw); err != nil {
		return nil, fmt.Errorf("parsing %s@%s: %w", name, version, err)
	}
	if fw.Name == "" {
		return nil, fmt.Errorf("invalid framework definition for %s@%s: missing name", name, version)
	}

	// Write to cache
	if mkErr := os.MkdirAll(filepath.Dir(cacheFile), 0o755); mkErr == nil {
		os.WriteFile(cacheFile, data, 0o644) //nolint:errcheck
	}

	return &fw, nil
}

// Search filters the store index by a case-insensitive substring match on name or label.
func (c *Client) Search(query string) ([]IndexEntry, error) {
	idx, err := c.FetchIndex()
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)
	var results []IndexEntry
	for _, entry := range idx.Frameworks {
		if strings.Contains(strings.ToLower(entry.Name), q) ||
			strings.Contains(strings.ToLower(entry.Label), q) {
			results = append(results, entry)
		}
	}
	return results, nil
}

// DetectFromStore checks the store index for a framework matching the given
// project directory. Returns the matching entry, the resolved version, and true
// if found. The version is auto-detected from composer.lock when possible.
func (c *Client) DetectFromStore(dir string) (*IndexEntry, string, bool) {
	idx, err := c.FetchIndex()
	if err != nil {
		return nil, "", false
	}

	for i, entry := range idx.Frameworks {
		for _, rule := range entry.Detect {
			if config.MatchesRule(dir, rule) {
				version := c.resolveVersion(dir, &entry)
				return &idx.Frameworks[i], version, true
			}
		}
	}
	return nil, "", false
}

// InvalidateIndex removes the cached index so the next FetchIndex call hits the network.
func (c *Client) InvalidateIndex() {
	os.Remove(filepath.Join(c.CacheDir, "index.json")) //nolint:errcheck
}

// resolveVersion tries to detect the framework version from composer.lock,
// falling back to the entry's Latest version.
func (c *Client) resolveVersion(dir string, entry *IndexEntry) string {
	// Find a composer package from the detect rules
	for _, rule := range entry.Detect {
		if rule.Composer != "" {
			if ver := DetectFrameworkVersion(dir, rule.Composer); ver != "" {
				// Check if this version exists in the store
				for _, v := range entry.Versions {
					if v == ver {
						return ver
					}
				}
			}
		}
	}
	return entry.Latest
}

func (c *Client) findEntry(idx *Index, name string) (*IndexEntry, bool) {
	for i, entry := range idx.Frameworks {
		if entry.Name == name {
			return &idx.Frameworks[i], true
		}
	}
	return nil, false
}

func (c *Client) fetch(path string) ([]byte, error) {
	url := c.BaseURL + "/" + path
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest(http.MethodGet, url, nil) //nolint:noctx
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "lerd-cli")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(resp.Body)
}
