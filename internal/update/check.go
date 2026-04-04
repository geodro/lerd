package update

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
)

const changelogRawURL = "https://raw.githubusercontent.com/geodro/lerd/main/CHANGELOG.md"

// UpdateInfo holds the result of a successful update check when a newer version exists.
type UpdateInfo struct {
	LatestVersion string // e.g. "v0.8.5"
	Changelog     string // relevant CHANGELOG.md sections (trimmed markdown)
}

type updateCheckState struct {
	LatestVersion string    `json:"version"`
	CheckedAt     time.Time `json:"checked_at"`
}

// CachedUpdateCheck returns update info when a newer version is available.
// Returns nil, nil if already on the latest version, or if the check fails silently
// (no network, GitHub unreachable, etc.). Network fetches are rate-limited to once
// per 24 hours via a cache file at config.UpdateCheckFile().
func CachedUpdateCheck(currentVersion string) (*UpdateInfo, error) {
	latest := cachedLatest()
	if latest == "" {
		return nil, nil
	}

	if !VersionGreaterThan(StripV(latest), StripV(currentVersion)) {
		return nil, nil
	}

	changelog, _ := FetchChangelog(StripV(currentVersion), StripV(latest))
	return &UpdateInfo{
		LatestVersion: latest,
		Changelog:     changelog,
	}, nil
}

// cachedLatest returns the latest release version tag, using a 24-hour disk cache.
// Returns "" on any error so callers degrade silently.
func cachedLatest() string {
	cacheFile := config.UpdateCheckFile()

	if data, err := os.ReadFile(cacheFile); err == nil {
		var state updateCheckState
		if json.Unmarshal(data, &state) == nil && time.Since(state.CheckedAt) < 24*time.Hour {
			return state.LatestVersion
		}
	}

	latest, err := FetchLatestVersion()
	if err != nil {
		// Cache the failure for 1 hour to avoid hammering GitHub on every invocation.
		writeCache(cacheFile, updateCheckState{
			LatestVersion: "",
			CheckedAt:     time.Now().Add(-23 * time.Hour),
		})
		return ""
	}

	writeCache(cacheFile, updateCheckState{LatestVersion: latest, CheckedAt: time.Now()})
	return latest
}

func writeCache(path string, state updateCheckState) {
	data, _ := json.Marshal(state)
	os.WriteFile(path, data, 0o644) //nolint:errcheck
}

// WriteUpdateCache records version as the known latest in the on-disk cache,
// resetting the 24-hour TTL. Call this after a successful update so that
// lerd status / doctor stop showing a stale "update available" notice.
func WriteUpdateCache(version string) {
	writeCache(config.UpdateCheckFile(), updateCheckState{
		LatestVersion: version,
		CheckedAt:     time.Now(),
	})
}

// FetchChangelog downloads CHANGELOG.md from GitHub and returns the sections
// for versions strictly greater than currentVersion and <= latestVersion.
// Returns an empty string and a non-nil error when the fetch fails.
func FetchChangelog(currentVersion, latestVersion string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, changelogRawURL, nil) //nolint:noctx
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "lerd-cli")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d fetching changelog", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return extractChangelogSections(string(body), currentVersion, latestVersion), nil
}

// extractChangelogSections parses changelog markdown and returns sections where
// the version header satisfies: currentVersion < section <= latestVersion.
func extractChangelogSections(changelog, currentVersion, latestVersion string) string {
	var result strings.Builder
	inSection := false

	scanner := bufio.NewScanner(strings.NewReader(changelog))
	for scanner.Scan() {
		line := scanner.Text()

		// Version header pattern: ## [X.Y.Z] — date
		if strings.HasPrefix(line, "## [") {
			inSection = false
			rest := strings.TrimPrefix(line, "## [")
			closeBracket := strings.Index(rest, "]")
			if closeBracket < 0 {
				continue
			}
			sectionVer := rest[:closeBracket]
			if VersionGreaterThan(sectionVer, currentVersion) && !VersionGreaterThan(sectionVer, latestVersion) {
				inSection = true
			}
		}

		if inSection {
			result.WriteString(line)
			result.WriteByte('\n')
		}
	}

	return strings.TrimSpace(result.String())
}

// VersionGreaterThan returns true if a > b, comparing "X.Y.Z[-pre]" version
// strings (without a leading "v") component-by-component as integers.
// Pre-release suffixes (e.g. "-beta.1", "-rc.1") are handled per semver:
// a release without a pre-release suffix is greater than one with the same
// core version, and pre-release suffixes are compared lexicographically.
func VersionGreaterThan(a, b string) bool {
	aCore, aPre := splitPrerelease(a)
	bCore, bPre := splitPrerelease(b)

	aParts := strings.Split(aCore, ".")
	bParts := strings.Split(bCore, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		var ai, bi int
		if i < len(aParts) {
			ai, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bi, _ = strconv.Atoi(bParts[i])
		}
		if ai != bi {
			return ai > bi
		}
	}

	// Core versions are equal — compare pre-release suffixes.
	// No pre-release > has pre-release (stable wins).
	if aPre == "" && bPre != "" {
		return true
	}
	if aPre != "" && bPre == "" {
		return false
	}
	return aPre > bPre
}

// splitPrerelease splits "1.2.3-beta.1" into ("1.2.3", "beta.1").
// If there is no pre-release suffix, pre is "".
func splitPrerelease(v string) (core, pre string) {
	if i := strings.IndexByte(v, '-'); i != -1 {
		return v[:i], v[i+1:]
	}
	return v, ""
}
