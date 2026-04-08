package applog

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
)

// LogEntry represents a single parsed log entry.
type LogEntry struct {
	Level   string `json:"level"`
	Date    string `json:"date"`
	Channel string `json:"channel"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// LogFile represents an available log file on disk.
type LogFile struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

// maxReadBytes is the maximum number of bytes to read from the end of a file.
const maxReadBytes = 512 * 1024

// laravelRe matches the opening line of a Laravel/Monolog log entry:
// [2024-01-09 13:13:49] local.ERROR: message text
var laravelRe = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\] (\w+)\.(\w+): (.*)$`)

// DiscoverLogFiles expands the configured log source globs for a project and
// returns the matching files sorted by modification time (newest first).
// Resolved paths are validated to stay within projectDir.
func DiscoverLogFiles(projectDir string, sources []config.FrameworkLogSource) ([]LogFile, error) {
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var files []LogFile

	for _, src := range sources {
		pattern := filepath.Join(absProject, src.Path)
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			abs, err := filepath.Abs(m)
			if err != nil || !strings.HasPrefix(abs, absProject+string(filepath.Separator)) {
				continue // path traversal guard
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true

			info, err := os.Stat(abs)
			if err != nil || info.IsDir() {
				continue
			}
			files = append(files, LogFile{
				Name:    info.Name(),
				Size:    info.Size(),
				ModTime: info.ModTime().UTC().Format(time.RFC3339),
			})
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime > files[j].ModTime
	})
	return files, nil
}

// LatestModTime returns the most recent modification time across all log files
// matched by the given sources. Returns zero time if no files found.
func LatestModTime(projectDir string, sources []config.FrameworkLogSource) time.Time {
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return time.Time{}
	}

	var latest time.Time
	for _, src := range sources {
		pattern := filepath.Join(absProject, src.Path)
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			abs, err := filepath.Abs(m)
			if err != nil || !strings.HasPrefix(abs, absProject+string(filepath.Separator)) {
				continue
			}
			info, err := os.Stat(abs)
			if err != nil || info.IsDir() {
				continue
			}
			if info.ModTime().After(latest) {
				latest = info.ModTime()
			}
		}
	}
	return latest
}

// ResolveLogFilePath finds the full path for a log file by name within the
// configured sources. Returns empty string if not found or path traversal detected.
func ResolveLogFilePath(projectDir string, sources []config.FrameworkLogSource, filename string) string {
	if strings.ContainsAny(filename, "/\\") || strings.Contains(filename, "..") {
		return ""
	}

	absProject, _ := filepath.Abs(projectDir)
	for _, src := range sources {
		pattern := filepath.Join(absProject, src.Path)
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			abs, err := filepath.Abs(m)
			if err != nil || !strings.HasPrefix(abs, absProject+string(filepath.Separator)) {
				continue
			}
			if filepath.Base(abs) == filename {
				return abs
			}
		}
	}
	return ""
}

// FormatForFile returns the log format string for the given filename based on
// the framework's log sources. Falls back to "raw".
func FormatForFile(sources []config.FrameworkLogSource, filename string) string {
	for _, src := range sources {
		// Check if the filename could match this source's glob pattern
		_, base := filepath.Split(src.Path)
		if matched, _ := filepath.Match(base, filename); matched {
			if src.Format != "" {
				return src.Format
			}
			return "raw"
		}
	}
	return "raw"
}

// ParseFile reads the tail of a log file and parses up to maxEntries entries.
// When maxEntries is 0, the entire file is read and all entries are returned.
// Returns entries in reverse-chronological order (newest first).
func ParseFile(path, format string, maxEntries int) ([]LogEntry, error) {
	readLimit := maxReadBytes
	if maxEntries == 0 {
		readLimit = 0 // read entire file
	}
	data, err := readTail(path, int64(readLimit))
	if err != nil {
		return nil, err
	}

	switch format {
	case "laravel", "monolog":
		return parseLaravel(string(data), maxEntries), nil
	default:
		return parseRaw(string(data), maxEntries), nil
	}
}

// readTail reads up to maxBytes from the end of the file. If the file is
// smaller than maxBytes, the entire file is read. When truncated, the first
// partial line is discarded. When maxBytes is 0, the entire file is read.
func readTail(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	truncated := false
	readSize := size
	if maxBytes > 0 && size > maxBytes {
		readSize = maxBytes
		truncated = true
		if _, err := f.Seek(size-maxBytes, 0); err != nil {
			return nil, err
		}
	}

	buf := make([]byte, readSize)
	n, err := f.Read(buf)
	if err != nil {
		return nil, err
	}
	buf = buf[:n]

	// Discard incomplete first line when truncated
	if truncated {
		if idx := indexByte(buf, '\n'); idx >= 0 {
			buf = buf[idx+1:]
		}
	}

	return buf, nil
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// parseLaravel splits Monolog-formatted log content into structured entries.
func parseLaravel(content string, maxEntries int) []LogEntry {
	lines := strings.Split(content, "\n")

	// Group lines into blocks: each block starts with a line matching laravelRe
	type block struct {
		header string
		extra  []string
	}
	var blocks []block

	for _, line := range lines {
		if laravelRe.MatchString(line) {
			blocks = append(blocks, block{header: line})
		} else if len(blocks) > 0 && strings.TrimSpace(line) != "" {
			blocks[len(blocks)-1].extra = append(blocks[len(blocks)-1].extra, line)
		}
	}

	// Take only the last maxEntries blocks (0 = unlimited)
	if maxEntries > 0 && len(blocks) > maxEntries {
		blocks = blocks[len(blocks)-maxEntries:]
	}

	// Parse blocks into entries, newest first
	entries := make([]LogEntry, 0, len(blocks))
	for i := len(blocks) - 1; i >= 0; i-- {
		b := blocks[i]
		m := laravelRe.FindStringSubmatch(b.header)
		if m == nil {
			continue
		}

		entry := LogEntry{
			Date:    m[1],
			Channel: m[2],
			Level:   strings.ToUpper(m[3]),
			Message: m[4],
		}

		if len(b.extra) > 0 {
			entry.Detail = entry.Message + "\n" + strings.Join(b.extra, "\n")
		}

		entries = append(entries, entry)
	}

	return entries
}

// parseRaw treats each non-empty line as a separate log entry.
func parseRaw(content string, maxEntries int) []LogEntry {
	lines := strings.Split(content, "\n")

	// Collect non-empty lines
	var nonEmpty []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}

	if maxEntries > 0 && len(nonEmpty) > maxEntries {
		nonEmpty = nonEmpty[len(nonEmpty)-maxEntries:]
	}

	entries := make([]LogEntry, 0, len(nonEmpty))
	for i := len(nonEmpty) - 1; i >= 0; i-- {
		entries = append(entries, LogEntry{
			Message: nonEmpty[i],
		})
	}
	return entries
}
