package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
)

const sampleLaravelLog = `[2024-01-08 14:05:08] local.ERROR: Database file at path [herd_templates] does not exist. Ensure this is an absolute path to the database. (Connection: sqlite, SQL: select * from "users") {"exception":"[object] (Illuminate\\Database\\QueryException(code: 0): Database file at path [herd_templates] does not exist.)"}
[stacktrace]
#0 /Users/seb/Code/herd-templates/vendor/laravel/framework/src/Illuminate/Database/Connection.php(801): Illuminate\Database\Connection->runQueryCallback()
#1 /Users/seb/Code/herd-templates/vendor/laravel/framework/src/Illuminate/Database/Connection.php(756): Illuminate\Database\Connection->run()
[2024-01-09 13:13:49] local.ERROR: syntax error, unexpected token "++", expecting variable or "{" or "$" {"exception":"[object] (ParseError(code: 0): syntax error)"}
[stacktrace]
#0 /Users/seb/Code/herd-templates/vendor/laravel/framework/src/Illuminate/Routing/Router.php(511): Illuminate\Routing\Router->loadRoutes()
#1 /Users/seb/Code/herd-templates/vendor/laravel/framework/src/Illuminate/Routing/Router.php(465): Illuminate\Routing\Router->loadRoutes()
[2024-01-09 13:14:00] local.ERROR: Maximum execution time of 30 seconds exceeded {"exception":"[object] (Symfony\\Component\\ErrorHandler\\Error\\FatalError)"}
[2024-01-11 14:58:09] local.ERROR: Connection could not be established with host "mailpit:1025": stream_socket_client(): php_network_getaddresses: getaddrinfo for mailpit failed {"exception":"[object] (Symfony\\Component\\Mailer\\Exception\\TransportException)"}
`

// ── parseLaravel ─────────────────────────────────────────────────────────────

func TestParseLaravel(t *testing.T) {
	entries := parseLaravel(sampleLaravelLog, 100)

	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	// Newest first
	if entries[0].Date != "2024-01-11 14:58:09" {
		t.Errorf("expected newest entry first, got date %s", entries[0].Date)
	}
	if entries[0].Level != "ERROR" {
		t.Errorf("expected level ERROR, got %s", entries[0].Level)
	}
	if entries[0].Channel != "local" {
		t.Errorf("expected channel local, got %s", entries[0].Channel)
	}
	if !strings.Contains(entries[0].Message, "mailpit:1025") {
		t.Errorf("expected message to contain mailpit:1025, got %s", entries[0].Message)
	}

	// Entry with stacktrace should have detail
	syntaxErr := entries[2] // 2024-01-09 13:13:49
	if syntaxErr.Detail == "" {
		t.Error("expected detail for entry with stacktrace")
	}
	if !strings.Contains(syntaxErr.Detail, "Router.php(511)") {
		t.Error("expected stacktrace in detail")
	}

	// Entry without extra lines should have no detail
	if entries[0].Detail != "" {
		t.Errorf("expected no detail for single-line entry, got %q", entries[0].Detail)
	}
}

func TestParseLaravelMaxEntries(t *testing.T) {
	entries := parseLaravel(sampleLaravelLog, 2)

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (limited), got %d", len(entries))
	}

	// Should be the 2 newest
	if entries[0].Date != "2024-01-11 14:58:09" {
		t.Errorf("expected newest entry, got %s", entries[0].Date)
	}
	if entries[1].Date != "2024-01-09 13:14:00" {
		t.Errorf("expected second newest entry, got %s", entries[1].Date)
	}
}

func TestParseLaravelEmpty(t *testing.T) {
	entries := parseLaravel("", 100)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for empty input, got %d", len(entries))
	}
}

func TestParseLaravelMultipleLevels(t *testing.T) {
	content := `[2024-03-01 10:00:00] production.DEBUG: Debug message
[2024-03-01 10:00:01] production.INFO: Info message
[2024-03-01 10:00:02] production.WARNING: Warning message
[2024-03-01 10:00:03] production.ERROR: Error message
[2024-03-01 10:00:04] production.CRITICAL: Critical message
[2024-03-01 10:00:05] production.ALERT: Alert message
[2024-03-01 10:00:06] production.EMERGENCY: Emergency message
`
	entries := parseLaravel(content, 100)
	if len(entries) != 7 {
		t.Fatalf("expected 7 entries, got %d", len(entries))
	}

	expectedLevels := []string{"EMERGENCY", "ALERT", "CRITICAL", "ERROR", "WARNING", "INFO", "DEBUG"}
	for i, want := range expectedLevels {
		if entries[i].Level != want {
			t.Errorf("entry %d: level = %q, want %q", i, entries[i].Level, want)
		}
	}

	for _, e := range entries {
		if e.Channel != "production" {
			t.Errorf("expected channel production, got %s", e.Channel)
		}
	}
}

func TestParseLaravelMultilineStacktrace(t *testing.T) {
	content := `[2024-03-01 10:00:00] local.ERROR: Something broke {"exception":"[object] (RuntimeException)"}
[stacktrace]
#0 /app/Http/Controller.php(42): App\Service->doStuff()
#1 /vendor/laravel/framework/Router.php(100): App\Http\Controller->handle()
#2 {main}
`
	entries := parseLaravel(content, 100)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	if e.Detail == "" {
		t.Fatal("expected detail with stacktrace")
	}
	if !strings.Contains(e.Detail, "#0") {
		t.Error("detail should contain stacktrace frame #0")
	}
	if !strings.Contains(e.Detail, "#2 {main}") {
		t.Error("detail should contain final stacktrace frame")
	}
	if !strings.Contains(e.Detail, "Something broke") {
		t.Error("detail should include the original message")
	}
}

func TestParseLaravelDetailIncludesMessage(t *testing.T) {
	content := `[2024-03-01 10:00:00] local.ERROR: The message line {"ctx":"val"}
extra context line
`
	entries := parseLaravel(content, 100)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !strings.HasPrefix(entries[0].Detail, entries[0].Message) {
		t.Error("detail should start with the message text")
	}
}

func TestParseLaravelOnlyHeaders(t *testing.T) {
	content := `[2024-03-01 10:00:00] local.INFO: first
[2024-03-01 10:00:01] local.INFO: second
[2024-03-01 10:00:02] local.INFO: third
`
	entries := parseLaravel(content, 100)
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Detail != "" {
			t.Errorf("expected no detail for header-only entry, got %q", e.Detail)
		}
	}
}

func TestParseLaravelGarbageLinesBeforeFirstEntry(t *testing.T) {
	content := `some garbage line
another garbage
[2024-03-01 10:00:00] local.INFO: real entry
`
	entries := parseLaravel(content, 100)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (garbage ignored), got %d", len(entries))
	}
	if entries[0].Message != "real entry" {
		t.Errorf("unexpected message: %q", entries[0].Message)
	}
}

func TestParseLaravelMaxEntriesOne(t *testing.T) {
	entries := parseLaravel(sampleLaravelLog, 1)
	if len(entries) != 1 {
		t.Fatalf("expected 1, got %d", len(entries))
	}
	if entries[0].Date != "2024-01-11 14:58:09" {
		t.Errorf("expected the newest entry, got %s", entries[0].Date)
	}
}

// ── parseRaw ─────────────────────────────────────────────────────────────────

func TestParseRaw(t *testing.T) {
	content := "line one\nline two\nline three\n"
	entries := parseRaw(content, 100)

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Newest first (last line first)
	if entries[0].Message != "line three" {
		t.Errorf("expected 'line three', got %q", entries[0].Message)
	}
	if entries[2].Message != "line one" {
		t.Errorf("expected 'line one', got %q", entries[2].Message)
	}
}

func TestParseRawMaxEntries(t *testing.T) {
	content := "a\nb\nc\nd\ne\n"
	entries := parseRaw(content, 2)

	if len(entries) != 2 {
		t.Fatalf("expected 2, got %d", len(entries))
	}
	if entries[0].Message != "e" {
		t.Errorf("expected 'e', got %q", entries[0].Message)
	}
	if entries[1].Message != "d" {
		t.Errorf("expected 'd', got %q", entries[1].Message)
	}
}

func TestParseRawEmpty(t *testing.T) {
	entries := parseRaw("", 100)
	if len(entries) != 0 {
		t.Fatalf("expected 0, got %d", len(entries))
	}
}

func TestParseRawBlankLines(t *testing.T) {
	content := "line one\n\n\nline two\n   \nline three\n"
	entries := parseRaw(content, 100)
	if len(entries) != 3 {
		t.Fatalf("expected 3 (blanks skipped), got %d", len(entries))
	}
}

func TestParseRawNoTrailingNewline(t *testing.T) {
	content := "alpha\nbeta"
	entries := parseRaw(content, 100)
	if len(entries) != 2 {
		t.Fatalf("expected 2, got %d", len(entries))
	}
	if entries[0].Message != "beta" {
		t.Errorf("expected beta first (newest), got %q", entries[0].Message)
	}
}

func TestParseRawFieldsEmpty(t *testing.T) {
	entries := parseRaw("hello\n", 100)
	if len(entries) != 1 {
		t.Fatal("expected 1")
	}
	e := entries[0]
	if e.Level != "" || e.Date != "" || e.Channel != "" || e.Detail != "" {
		t.Error("raw entries should only have Message set")
	}
}

// ── ParseFile ────────────────────────────────────────────────────────────────

func TestParseFileMonolog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "laravel.log")
	os.WriteFile(path, []byte(sampleLaravelLog), 0644)

	entries, err := ParseFile(path, "monolog", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
}

func TestParseFileLaravelAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	os.WriteFile(path, []byte("[2024-01-01 00:00:00] local.INFO: test\n"), 0644)

	entries, err := ParseFile(path, "laravel", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry with 'laravel' alias, got %d", len(entries))
	}
}

func TestParseFileRaw(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0644)

	entries, err := ParseFile(path, "raw", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
}

func TestParseFileUnknownFormatFallsBackToRaw(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	os.WriteFile(path, []byte("some text\n"), 0644)

	entries, err := ParseFile(path, "unknown_format", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 (raw fallback), got %d", len(entries))
	}
}

func TestParseFileEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	os.WriteFile(path, []byte(""), 0644)

	entries, err := ParseFile(path, "monolog", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0, got %d", len(entries))
	}
}

func TestParseFileNonExistent(t *testing.T) {
	_, err := ParseFile("/nonexistent/path/file.log", "monolog", 100)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// ── readTail ─────────────────────────────────────────────────────────────────

func TestReadTailSmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.log")
	content := "line one\nline two\n"
	os.WriteFile(path, []byte(content), 0644)

	data, err := readTail(path, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("expected full content, got %q", string(data))
	}
}

func TestReadTailTruncatesLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")

	var content strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&content, "[2024-01-01 00:00:00] local.INFO: line %d %s\n", i, strings.Repeat("x", 100))
	}
	os.WriteFile(path, []byte(content.String()), 0644)

	data, err := readTail(path, 1024) // only read last 1KB
	if err != nil {
		t.Fatal(err)
	}

	if int64(len(data)) >= 1024 {
		t.Errorf("expected data shorter than maxBytes after partial line discard, got %d", len(data))
	}

	// First line should be complete (partial line discarded)
	lines := strings.SplitN(string(data), "\n", 2)
	if len(lines) > 0 && lines[0] != "" {
		if !strings.HasPrefix(lines[0], "[") {
			t.Errorf("first line looks partial after truncation: %q", lines[0])
		}
	}
}

func TestReadTailEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	os.WriteFile(path, []byte(""), 0644)

	data, err := readTail(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if data != nil {
		t.Errorf("expected nil for empty file, got %q", string(data))
	}
}

// ── DiscoverLogFiles ─────────────────────────────────────────────────────────

func TestDiscoverLogFiles(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "storage", "logs")
	os.MkdirAll(logDir, 0755)

	oldTime := time.Now().Add(-time.Hour)
	os.WriteFile(filepath.Join(logDir, "laravel.log"), []byte("test"), 0644)
	os.Chtimes(filepath.Join(logDir, "laravel.log"), oldTime, oldTime)
	os.WriteFile(filepath.Join(logDir, "worker.log"), []byte("test2"), 0644)
	// worker.log keeps the current time, laravel.log is 1 hour old

	sources := []config.FrameworkLogSource{
		{Path: "storage/logs/*.log", Format: "monolog"},
	}

	files, err := DiscoverLogFiles(dir, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	// Newest first
	if files[0].Name != "worker.log" {
		t.Errorf("expected worker.log first (newest), got %s", files[0].Name)
	}
}

func TestDiscoverLogFilesMultipleSources(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "storage", "logs"), 0755)
	os.MkdirAll(filepath.Join(dir, "var", "log"), 0755)
	os.WriteFile(filepath.Join(dir, "storage", "logs", "app.log"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "var", "log", "debug.log"), []byte("b"), 0644)

	sources := []config.FrameworkLogSource{
		{Path: "storage/logs/*.log", Format: "monolog"},
		{Path: "var/log/*.log", Format: "raw"},
	}

	files, err := DiscoverLogFiles(dir, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files from 2 sources, got %d", len(files))
	}
}

func TestDiscoverLogFilesDeduplicates(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0755)
	os.WriteFile(filepath.Join(logDir, "app.log"), []byte("x"), 0644)

	sources := []config.FrameworkLogSource{
		{Path: "logs/*.log"},
		{Path: "logs/app.log"}, // overlapping glob
	}

	files, err := DiscoverLogFiles(dir, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file (deduplicated), got %d", len(files))
	}
}

func TestDiscoverLogFilesPathTraversal(t *testing.T) {
	dir := t.TempDir()
	sources := []config.FrameworkLogSource{
		{Path: "../../../etc/*.log"},
	}

	files, err := DiscoverLogFiles(dir, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files for traversal attempt, got %d", len(files))
	}
}

func TestDiscoverLogFilesNoMatches(t *testing.T) {
	dir := t.TempDir()
	sources := []config.FrameworkLogSource{
		{Path: "nonexistent/*.log"},
	}

	files, err := DiscoverLogFiles(dir, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}

func TestDiscoverLogFilesSkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0755)
	os.WriteFile(filepath.Join(logDir, "app.log"), []byte("x"), 0644)
	// Create a directory that matches the glob
	os.MkdirAll(filepath.Join(logDir, "subdir.log"), 0755)

	sources := []config.FrameworkLogSource{
		{Path: "logs/*.log"},
	}

	files, err := DiscoverLogFiles(dir, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file (directory skipped), got %d", len(files))
	}
}

func TestDiscoverLogFilesReportsSize(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0755)
	content := strings.Repeat("x", 42)
	os.WriteFile(filepath.Join(logDir, "app.log"), []byte(content), 0644)

	sources := []config.FrameworkLogSource{{Path: "logs/*.log"}}
	files, err := DiscoverLogFiles(dir, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatal("expected 1 file")
	}
	if files[0].Size != 42 {
		t.Errorf("size = %d, want 42", files[0].Size)
	}
}

func TestDiscoverLogFilesEmptySources(t *testing.T) {
	dir := t.TempDir()
	files, err := DiscoverLogFiles(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 for nil sources, got %d", len(files))
	}
}

// ── ResolveLogFilePath ───────────────────────────────────────────────────────

func TestResolveLogFilePath(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "storage", "logs")
	os.MkdirAll(logDir, 0755)
	os.WriteFile(filepath.Join(logDir, "laravel.log"), []byte("test"), 0644)

	sources := []config.FrameworkLogSource{
		{Path: "storage/logs/*.log", Format: "monolog"},
	}

	path := ResolveLogFilePath(dir, sources, "laravel.log")
	if path == "" {
		t.Fatal("expected to resolve laravel.log")
	}
	if filepath.Base(path) != "laravel.log" {
		t.Errorf("resolved to %q, expected laravel.log basename", path)
	}
}

func TestResolveLogFilePathTraversal(t *testing.T) {
	dir := t.TempDir()
	sources := []config.FrameworkLogSource{{Path: "logs/*.log"}}

	tests := []struct {
		name     string
		filename string
	}{
		{"dotdot", "../etc/passwd"},
		{"slash", "sub/file.log"},
		{"backslash", "sub\\file.log"},
		{"dotdot_only", ".."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveLogFilePath(dir, sources, tc.filename); got != "" {
				t.Errorf("expected empty for %q, got %q", tc.filename, got)
			}
		})
	}
}

func TestResolveLogFilePathNotFound(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0755)
	os.WriteFile(filepath.Join(logDir, "app.log"), []byte("x"), 0644)

	sources := []config.FrameworkLogSource{{Path: "logs/*.log"}}

	if got := ResolveLogFilePath(dir, sources, "nope.log"); got != "" {
		t.Errorf("expected empty for nonexistent file, got %q", got)
	}
}

func TestResolveLogFilePathMultipleSources(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a"), 0755)
	os.MkdirAll(filepath.Join(dir, "b"), 0755)
	os.WriteFile(filepath.Join(dir, "b", "found.log"), []byte("x"), 0644)

	sources := []config.FrameworkLogSource{
		{Path: "a/*.log"},
		{Path: "b/*.log"},
	}

	path := ResolveLogFilePath(dir, sources, "found.log")
	if path == "" {
		t.Fatal("expected to find file in second source")
	}
}

// ── FormatForFile ────────────────────────────────────────────────────────────

func TestFormatForFile(t *testing.T) {
	sources := []config.FrameworkLogSource{
		{Path: "storage/logs/*.log", Format: "monolog"},
		{Path: "var/log/*.txt", Format: "raw"},
	}

	if f := FormatForFile(sources, "laravel.log"); f != "monolog" {
		t.Errorf("expected monolog, got %s", f)
	}
	if f := FormatForFile(sources, "app.txt"); f != "raw" {
		t.Errorf("expected raw, got %s", f)
	}
}

func TestFormatForFileNoMatch(t *testing.T) {
	sources := []config.FrameworkLogSource{
		{Path: "logs/*.log", Format: "monolog"},
	}
	if f := FormatForFile(sources, "other.json"); f != "raw" {
		t.Errorf("expected raw fallback for non-matching file, got %s", f)
	}
}

func TestFormatForFileEmptyFormat(t *testing.T) {
	sources := []config.FrameworkLogSource{
		{Path: "logs/*.log"}, // no format set
	}
	if f := FormatForFile(sources, "app.log"); f != "raw" {
		t.Errorf("expected raw when format is empty, got %s", f)
	}
}

func TestFormatForFileEmptySources(t *testing.T) {
	if f := FormatForFile(nil, "app.log"); f != "raw" {
		t.Errorf("expected raw for nil sources, got %s", f)
	}
}
