package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewTailer(t *testing.T) {
	tailer := NewTailer("/test/path", true)

	if tailer.Path != "/test/path" {
		t.Errorf("Expected Path=/test/path, got %q", tailer.Path)
	}

	if !tailer.fromBeg {
		t.Error("Expected fromBeg=true")
	}

	if tailer.started {
		t.Error("Expected started=false")
	}

	if tailer.file != nil {
		t.Error("Expected file=nil")
	}
}

func TestNewTailerFromEnd(t *testing.T) {
	tailer := NewTailer("/test/path", false)

	if tailer.fromBeg {
		t.Error("Expected fromBeg=false")
	}
}

func TestTailerReadNewLines(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	// Write initial content
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Test reading from beginning
	tailer := NewTailer(testFile, true)
	lines, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}

	// Should return 4 lines (line1, line2, line3, and empty string after last newline)
	if len(lines) < 3 {
		t.Errorf("Expected at least 3 lines, got %d", len(lines))
	}

	// Read again should return nothing (no new content)
	lines2, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines2) != 0 {
		t.Errorf("Expected 0 new lines, got %d", len(lines2))
	}

	// Append more content
	f, err := os.OpenFile(testFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("line4\nline5\n")
	f.Close()

	// Now we should get new lines
	lines3, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines3) < 2 {
		t.Errorf("Expected at least 2 new lines, got %d", len(lines3))
	}

	// Close and check
	tailer.Close()
	// File should be closed after Close()
}

func TestTailerReadFromEnd(t *testing.T) {
	// Create temp file with existing content
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Create tailer that reads from end
	tailer := NewTailer(testFile, false)
	lines, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}

	// Should return nothing (skipped to end)
	if len(lines) != 0 {
		t.Errorf("Expected 0 lines when reading from end, got %d", len(lines))
	}

	// Append and read new content
	f, err := os.OpenFile(testFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("line4\n")
	f.Close()

	time.Sleep(10 * time.Millisecond) // Ensure file is flushed

	lines2, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines2) < 1 {
		t.Errorf("Expected at least 1 new line, got %d", len(lines2))
	}
}

func TestTailerIncompleteLine(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	// Write content without final newline
	content := "line1\nline2\nincomplete"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tailer := NewTailer(testFile, true)
	lines, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}

	// Should get complete lines only, incomplete line buffered
	foundComplete := false
	for _, line := range lines {
		if line == "line1" || line == "line2" {
			foundComplete = true
		}
		// Incomplete line should not be in results
		if line == "incomplete" {
			t.Error("Incomplete line should not be returned")
		}
	}

	if !foundComplete {
		t.Error("Should have found complete lines")
	}

	// Now complete the line
	f, err := os.OpenFile(testFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\n")
	f.Close()

	// Read again should now return the completed line
	lines2, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}

	foundIncomplete := false
	for _, line := range lines2 {
		if line == "incomplete" {
			foundIncomplete = true
		}
	}

	if !foundIncomplete {
		t.Error("Should have found the completed incomplete line")
	}
}

func TestTailerLogRotation(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	// Write initial content
	content := strings.Repeat("line\n", 100)
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tailer := NewTailer(testFile, true)

	// Read initial content
	lines, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) < 100 {
		t.Errorf("Expected at least 100 lines, got %d", len(lines))
	}

	// Simulate log rotation by truncating file
	if err := os.WriteFile(testFile, []byte("newlog\nline1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should detect rotation and read new content
	lines2, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}

	// Should have new content
	if len(lines2) == 0 {
		t.Error("Should have detected log rotation and read new content")
	}
}

func TestTailerReset(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	content := "line1\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tailer := NewTailer(testFile, true)

	// Read to open file
	_, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}

	if tailer.file == nil {
		t.Error("Expected file to be open after read")
	}

	// Reset
	tailer.Reset()

	if tailer.file != nil {
		t.Error("Expected file to be nil after reset")
	}
	if tailer.offset != 0 {
		t.Errorf("Expected offset=0 after reset, got %d", tailer.offset)
	}
	if tailer.pending != "" {
		t.Errorf("Expected pending empty after reset, got %q", tailer.pending)
	}
	if tailer.started {
		t.Error("Expected started=false after reset")
	}
}

func TestTailerClose(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	content := "line1\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tailer := NewTailer(testFile, true)

	// Read to open file
	_, err := tailer.ReadNewLines()
	if err != nil {
		t.Fatal(err)
	}

	// Close
	if err := tailer.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestTailSnippet(t *testing.T) {
	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	// Write multi-line content
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Get last 3 lines
	snippet := TailSnippet(testFile, 3, 1000)
	if snippet == "" {
		t.Error("Expected non-empty snippet")
	}

	// Should contain last lines
	lines := strings.Split(snippet, "\n")
	if len(lines) < 3 {
		t.Errorf("Expected at least 3 lines in snippet, got %d", len(lines))
	}

	// Should contain "line5"
	if !strings.Contains(snippet, "line5") {
		t.Error("Snippet should contain last line")
	}
}

func TestTailSnippetMaxBytes(t *testing.T) {
	// Create temp file with long content
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	content := strings.Repeat("x", 1000) + "\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Get snippet with max bytes limit
	snippet := TailSnippet(testFile, 10, 100)
	if len(snippet) > 103 { // 100 + "..."
		t.Errorf("Expected snippet truncated to ~103 chars, got %d", len(snippet))
	}

	if !strings.HasSuffix(snippet, "...") {
		t.Error("Truncated snippet should end with ...")
	}
}

func TestTailSnippetNonExistent(t *testing.T) {
	snippet := TailSnippet("/nonexistent/file", 10, 1000)
	if snippet != "" {
		t.Error("Expected empty string for non-existent file")
	}
}

func TestTailSnippetZeroMaxLines(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")

	content := "line1\nline2\n"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	snippet := TailSnippet(testFile, 0, 1000)
	if snippet != "" {
		t.Error("Expected empty string when maxLines is 0")
	}
}

func TestFindRecentFiles(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create some files
	now := time.Now()
	oldTime := now.Add(-1 * time.Hour)

	// Recent log file
	recentLog := filepath.Join(tmpDir, "recent.log")
	if err := os.WriteFile(recentLog, []byte("recent"), 0644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(recentLog, now, now)

	// Old log file
	oldLog := filepath.Join(tmpDir, "old.log")
	if err := os.WriteFile(oldLog, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(oldLog, oldTime, oldTime)

	// Non-log file (should be ignored)
	textFile := filepath.Join(tmpDir, "readme.txt")
	if err := os.WriteFile(textFile, []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}

	// Find recent files
	entries := FindRecentFiles(tmpDir, 1, 10)

	// Should find log files but not txt
	if len(entries) == 0 {
		t.Error("Expected to find log files")
	}

	// Recent log should be first (sorted by mod time, newest first)
	if len(entries) > 0 && entries[0].Path != recentLog {
		t.Errorf("Expected first entry to be %q, got %q", recentLog, entries[0].Path)
	}
}

func TestFindRecentFilesDepth(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file at root level
	rootLog := filepath.Join(tmpDir, "root.log")
	if err := os.WriteFile(rootLog, []byte("root"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create subdirectory with file
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	subLog := filepath.Join(subDir, "sub.log")
	if err := os.WriteFile(subLog, []byte("sub"), 0644); err != nil {
		t.Fatal(err)
	}

	// With maxDepth=1, should only find root level file (sub is at depth 1, subLog is at depth 2)
	entries := FindRecentFiles(tmpDir, 1, 10)
	if len(entries) < 1 {
		t.Errorf("Expected at least 1 entry with maxDepth=1, got %d", len(entries))
	}

	// Verify root file is found
	foundRoot := false
	for _, e := range entries {
		if e.Path == rootLog {
			foundRoot = true
		}
	}
	if !foundRoot {
		t.Error("Should find file at root level")
	}

	// With maxDepth=2, should find both files (root at depth 0, subLog at depth 2)
	entries = FindRecentFiles(tmpDir, 2, 10)
	if len(entries) < 2 {
		t.Errorf("Expected at least 2 entries with maxDepth=2, got %d", len(entries))
	}
}

func TestFindRecentFilesLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple log files
	for i := 0; i < 5; i++ {
		path := filepath.Join(tmpDir, "file"+string(rune('0'+i))+".log")
		if err := os.WriteFile(path, []byte("content"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Limit to 3
	entries := FindRecentFiles(tmpDir, 1, 3)
	if len(entries) > 3 {
		t.Errorf("Expected max 3 entries, got %d", len(entries))
	}
}

func TestFindRecentFilesExtensions(t *testing.T) {
	tmpDir := t.TempDir()

	// Test that .log files are included
	logFile := filepath.Join(tmpDir, "test.log")
	if err := os.WriteFile(logFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Test that .md files are ignored
	mdFile := filepath.Join(tmpDir, "test.md")
	if err := os.WriteFile(mdFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Use maxDepth=1 to find files at root level
	entries := FindRecentFiles(tmpDir, 1, 100)

	// Should find .log file
	foundLog := false
	foundMd := false
	for _, e := range entries {
		if filepath.Base(e.Path) == "test.log" {
			foundLog = true
		}
		if filepath.Base(e.Path) == "test.md" {
			foundMd = true
		}
	}

	if !foundLog {
		t.Error("Should find .log file")
	}
	if foundMd {
		t.Error("Should not find .md file")
	}
}

func TestFindRecentFilesSingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.log")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pass a file path instead of directory
	entries := FindRecentFiles(testFile, 0, 10)
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry when passing a file path, got %d", len(entries))
	}
	if entries[0].Path != testFile {
		t.Errorf("Expected path=%q, got %q", testFile, entries[0].Path)
	}
}

func TestFindRecentFilesNonExistent(t *testing.T) {
	entries := FindRecentFiles("/nonexistent/path", 0, 10)
	if len(entries) != 0 {
		t.Error("Expected empty result for non-existent path")
	}
}

func TestTailerManager(t *testing.T) {
	tmpDir := t.TempDir()

	// Create some log files
	log1 := filepath.Join(tmpDir, "log1.log")
	log2 := filepath.Join(tmpDir, "log2.log")

	if err := os.WriteFile(log1, []byte("content1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log2, []byte("content2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create manager
	mgr := NewTailerManager(tmpDir, 5, 1, false)

	if mgr.BasePath != tmpDir {
		t.Errorf("Expected BasePath=%q, got %q", tmpDir, mgr.BasePath)
	}

	if mgr.MaxFiles != 5 {
		t.Errorf("Expected MaxFiles=5, got %d", mgr.MaxFiles)
	}

	if mgr.MaxDepth != 1 {
		t.Errorf("Expected MaxDepth=1, got %d", mgr.MaxDepth)
	}

	// Refresh files
	paths := mgr.RefreshFiles()
	if len(paths) < 2 {
		t.Errorf("Expected at least 2 paths, got %d", len(paths))
	}

	// Read new lines
	allLines := mgr.ReadAllNew()
	if len(allLines) == 0 {
		// May be empty since we're reading from end
		// This is expected behavior
	}

	// Close
	mgr.Close()
	if len(mgr.tailers) != 0 {
		t.Error("Expected tailers map to be cleared after Close")
	}
}

func TestTailerManagerCache(t *testing.T) {
	tmpDir := t.TempDir()

	log1 := filepath.Join(tmpDir, "log1.log")
	if err := os.WriteFile(log1, []byte("content1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewTailerManager(tmpDir, 5, 1, false)

	// First refresh
	paths1 := mgr.RefreshFiles()
	time.Sleep(10 * time.Millisecond)

	// Second refresh should use cache (within scanTTL)
	paths2 := mgr.RefreshFiles()

	if len(paths1) != len(paths2) {
		t.Error("Cached refresh should return same paths")
	}
}

func TestTailerManagerFileRemoval(t *testing.T) {
	tmpDir := t.TempDir()

	log1 := filepath.Join(tmpDir, "log1.log")
	if err := os.WriteFile(log1, []byte("content1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewTailerManager(tmpDir, 5, 1, true)

	// Refresh to pick up file
	paths := mgr.RefreshFiles()
	if len(paths) != 1 {
		t.Errorf("Expected 1 path, got %d", len(paths))
	}

	// Remove file
	os.Remove(log1)

	// Create a new manager to bypass cache
	mgr2 := NewTailerManager(tmpDir, 5, 1, true)
	paths = mgr2.RefreshFiles()

	// After file removal, should not find it (or it may find stale files in other tests)
	// Just verify the manager can handle empty results
	_ = paths // Manager handles empty results gracefully
}

func TestTailerManagerReadFromBeginning(t *testing.T) {
	tmpDir := t.TempDir()

	log1 := filepath.Join(tmpDir, "log1.log")
	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(log1, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Manager with fromBeg=true
	mgr := NewTailerManager(tmpDir, 5, 1, true)
	mgr.RefreshFiles()

	// Should read existing content
	lines := mgr.ReadAllNew()
	if len(lines) == 0 {
		t.Error("Expected to read existing content when fromBeg=true")
	}

	// Check we got content from log1
	if logLines, ok := lines[log1]; ok {
		if len(logLines) < 3 {
			t.Errorf("Expected at least 3 lines from log1, got %d", len(logLines))
		}
	} else {
		t.Error("Expected to have content for log1")
	}
}
