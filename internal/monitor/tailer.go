package monitor

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"firebell/internal/util"
)

// Tailer reads new lines from a log file, tracking read position and handling
// log rotation. Uses buffer pooling to minimize allocations.
type Tailer struct {
	Path    string    // File path being tailed
	file    *os.File  // Open file handle
	offset  int64     // Current read position
	pending string    // Buffered incomplete line
	started bool      // Whether initial read/seek occurred
	fromBeg bool      // Read from beginning vs skip to end
}

// NewTailer creates a new Tailer for the given path.
// If fromBeginning is false, it will skip to the end of existing content.
func NewTailer(path string, fromBeginning bool) *Tailer {
	return &Tailer{
		Path:    path,
		fromBeg: fromBeginning,
	}
}

// ensureFile opens the file if not already open.
func (t *Tailer) ensureFile() error {
	if t.file != nil {
		return nil
	}

	f, err := os.Open(t.Path)
	if err != nil {
		return err
	}
	t.file = f
	t.offset = 0
	t.pending = ""

	// Skip to end if not reading from beginning (first open only)
	if !t.fromBeg && !t.started {
		if info, err := t.file.Stat(); err == nil {
			t.offset = info.Size()
			if _, err := t.file.Seek(t.offset, io.SeekStart); err != nil {
				t.offset = 0
				t.file.Seek(0, io.SeekStart)
			}
		}
	}
	t.started = true
	return nil
}

// Reset closes the file and resets state.
func (t *Tailer) Reset() {
	if t.file != nil {
		t.file.Close()
	}
	t.file = nil
	t.offset = 0
	t.pending = ""
	t.started = false
}

// Close closes the tailer.
func (t *Tailer) Close() error {
	if t.file != nil {
		return t.file.Close()
	}
	return nil
}

// ReadNewLines reads newly appended lines from the log file since last read.
// Returns complete lines only; incomplete lines are buffered.
// Detects log rotation by comparing file size to saved offset.
func (t *Tailer) ReadNewLines() ([]string, error) {
	if err := t.ensureFile(); err != nil {
		return nil, err
	}

	info, err := t.file.Stat()
	if err != nil {
		t.Reset()
		return nil, err
	}

	// Detect rotation: if file size is smaller than our offset
	if info.Size() < t.offset {
		t.Reset()
		if err := t.ensureFile(); err != nil {
			return nil, err
		}
	}

	// Nothing new to read
	if info.Size() == t.offset {
		return nil, nil
	}

	// Seek to last position
	if _, err := t.file.Seek(t.offset, io.SeekStart); err != nil {
		t.Reset()
		return nil, err
	}

	// Read new content using pooled buffer
	buf := util.GetBuffer()
	defer util.PutBuffer(buf)

	var accumulated bytes.Buffer
	reader := bufio.NewReader(t.file)

	for {
		n, readErr := reader.Read(*buf)
		if n > 0 {
			accumulated.Write((*buf)[:n])
			t.offset += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}

	// Split into lines, preserving pending partial line
	data := t.pending + accumulated.String()
	lines := strings.Split(data, "\n")

	// If data doesn't end with newline, buffer the incomplete line
	if !strings.HasSuffix(data, "\n") && len(lines) > 0 {
		t.pending = lines[len(lines)-1]
		lines = lines[:len(lines)-1]
	} else {
		t.pending = ""
	}

	return lines, nil
}

// TailSnippet reads the last N lines from a file for context.
func TailSnippet(path string, maxLines, maxBytes int) string {
	if maxLines <= 0 {
		return ""
	}

	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}

	// Read last chunk of file
	const readSize = 16 * 1024
	size := info.Size()
	start := size - readSize
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}

	content, err := io.ReadAll(f)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	snippet := strings.Join(lines, "\n")
	if maxBytes > 0 && len(snippet) > maxBytes {
		if maxBytes > 3 {
			return snippet[:maxBytes-3] + "..."
		}
		return snippet[:maxBytes]
	}
	return snippet
}

// FileEntry represents a file with its modification time.
type FileEntry struct {
	Path    string
	ModTime time.Time
}

// FindRecentFiles finds the most recently modified files in a directory.
// Returns up to limit files, sorted by modification time (newest first).
// Only includes files with allowed extensions: .log, .txt, .json, .jsonl
func FindRecentFiles(basePath string, maxDepth, limit int) []FileEntry {
	info, err := os.Stat(basePath)
	if err != nil {
		return nil
	}

	// If it's a file, check extension and return
	if !info.IsDir() {
		if hasLogExtension(basePath) {
			return []FileEntry{{Path: basePath, ModTime: info.ModTime()}}
		}
		return nil
	}

	// Walk directory and collect files
	var entries []FileEntry
	baseDepth := strings.Count(basePath, string(os.PathSeparator))

	filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			// Check depth for directories
			if info != nil && info.IsDir() {
				depth := strings.Count(path, string(os.PathSeparator)) - baseDepth
				if depth > maxDepth {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Check depth for files
		depth := strings.Count(path, string(os.PathSeparator)) - baseDepth
		if depth > maxDepth {
			return nil
		}

		// Check extension
		if !hasLogExtension(path) {
			return nil
		}

		entries = append(entries, FileEntry{Path: path, ModTime: info.ModTime()})
		return nil
	})

	// Sort by modification time (newest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModTime.After(entries[j].ModTime)
	})

	// Limit results
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	return entries
}

// TailerManager manages multiple tailers for an agent.
type TailerManager struct {
	BasePath   string
	MaxFiles   int
	MaxDepth   int
	FromBeg    bool
	tailers    map[string]*Tailer
	lastScan   time.Time
	scanTTL    time.Duration
}

// NewTailerManager creates a new tailer manager.
func NewTailerManager(basePath string, maxFiles, maxDepth int, fromBeg bool) *TailerManager {
	return &TailerManager{
		BasePath: basePath,
		MaxFiles: maxFiles,
		MaxDepth: maxDepth,
		FromBeg:  fromBeg,
		tailers:  make(map[string]*Tailer),
		scanTTL:  5 * time.Second, // Cache scan results for 5s
	}
}

// RefreshFiles updates the watched files based on recent activity.
// Uses caching to avoid rescanning on every call.
func (m *TailerManager) RefreshFiles() []string {
	// Check cache
	if time.Since(m.lastScan) < m.scanTTL {
		paths := make([]string, 0, len(m.tailers))
		for path := range m.tailers {
			paths = append(paths, path)
		}
		return paths
	}

	// Find recent files
	entries := FindRecentFiles(m.BasePath, m.MaxDepth, m.MaxFiles)
	m.lastScan = time.Now()

	// Build desired set
	desired := make(map[string]bool)
	for _, entry := range entries {
		desired[entry.Path] = true
	}

	// Remove tailers for files no longer desired
	for path, tailer := range m.tailers {
		if !desired[path] {
			tailer.Close()
			delete(m.tailers, path)
		}
	}

	// Add tailers for new files
	for path := range desired {
		if _, ok := m.tailers[path]; !ok {
			m.tailers[path] = NewTailer(path, m.FromBeg)
		}
	}

	// Return current paths
	paths := make([]string, 0, len(m.tailers))
	for path := range m.tailers {
		paths = append(paths, path)
	}
	return paths
}

// ReadAllNew reads new lines from all managed tailers.
// Returns a map of path -> lines.
func (m *TailerManager) ReadAllNew() map[string][]string {
	result := make(map[string][]string)

	for path, tailer := range m.tailers {
		lines, err := tailer.ReadNewLines()
		if err != nil {
			// Reset tailer on error
			tailer.Reset()
			continue
		}
		if len(lines) > 0 {
			result[path] = lines
		}
	}

	return result
}

// Close closes all managed tailers.
func (m *TailerManager) Close() {
	for _, tailer := range m.tailers {
		tailer.Close()
	}
	m.tailers = make(map[string]*Tailer)
}
