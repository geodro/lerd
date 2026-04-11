package store

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	defaultBaseURL = "https://raw.githubusercontent.com/geodro/lerd-frameworks/main/frameworks"
	httpTimeout    = 10 * time.Second
)

// Client fetches framework definitions from the remote store.
type Client struct {
	BaseURL string
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

func init() {
	config.RegisterFrameworkFetchHook(autoFetchFramework)
}

// autoFetchFramework downloads a framework definition from the store and saves
// it to the local store directory. Called automatically by config.GetFrameworkForDir
// when a version-specific definition is missing locally.
func autoFetchFramework(name, version string) (*config.Framework, error) {
	client := NewClient()
	fw, err := client.FetchFramework(name, version)
	if err != nil {
		return nil, err
	}
	if err := config.SaveStoreFramework(fw); err != nil {
		return nil, err
	}
	return fw, nil
}

// NewClient returns a store client with default settings.
func NewClient() *Client {
	return &Client{
		BaseURL: defaultBaseURL,
	}
}

// FetchIndex downloads the store index.
func (c *Client) FetchIndex() (*Index, error) {
	data, err := c.fetch("index.json")
	if err != nil {
		return nil, fmt.Errorf("fetching store index: %w", err)
	}

	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing store index: %w", err)
	}

	return &idx, nil
}

// FetchFramework downloads a framework definition from the store.
// Always fetches from remote to ensure definitions are up to date.
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

// ResolveVersion detects the framework version from detect rules, checking
// composer.json constraints and version_file regex matches. Returns the first
// version that matches one of the available versions, or fallback if none match.
func ResolveVersion(dir string, rules []config.FrameworkRule, available []string, fallback string) string {
	for _, rule := range rules {
		if rule.Composer != "" {
			if ver := DetectFrameworkVersionWithKey(dir, rule.Composer, rule.VersionKey, rule.ComposerSections...); ver != "" {
				for _, v := range available {
					if v == ver {
						return ver
					}
				}
			}
		}
		if rule.VersionFile != "" && rule.VersionPattern != "" {
			if ver := DetectVersionFromFile(dir, rule.VersionFile, rule.VersionPattern); ver != "" {
				for _, v := range available {
					if v == ver {
						return ver
					}
				}
			}
		}
	}
	return fallback
}

func (c *Client) resolveVersion(dir string, entry *IndexEntry) string {
	return ResolveVersion(dir, entry.Detect, entry.Versions, entry.Latest)
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
