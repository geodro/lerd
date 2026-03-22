package update

import (
	"bufio"
	"encoding/json"
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

	if !versionGreaterThan(StripV(latest), StripV(currentVersion)) {
		return nil, nil
	}

	changelog := FetchChangelog(StripV(currentVersion), StripV(latest))
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
func FetchChangelog(currentVersion, latestVersion string) string {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, changelogRawURL, nil) //nolint:noctx
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "lerd-cli")
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return extractChangelogSections(string(body), currentVersion, latestVersion)
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
			if versionGreaterThan(sectionVer, currentVersion) && !versionGreaterThan(sectionVer, latestVersion) {
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

// versionGreaterThan returns true if a > b, comparing "X.Y.Z" version strings
// (without a leading "v") component-by-component as integers.
func versionGreaterThan(a, b string) bool {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")

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
	return false
}
