package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogLevel represents log severity.
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// LogEntry represents a structured log entry.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Agent     string    `json:"agent,omitempty"`
	Event     string    `json:"event,omitempty"`
	Details   string    `json:"details,omitempty"`
}

// Logger handles logging to files with rotation.
type Logger struct {
	mu          sync.Mutex
	dir         string
	file        *os.File
	currentDate string
	minLevel    LogLevel
}

// NewLogger creates a new logger.
func NewLogger(dir string) (*Logger, error) {
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	l := &Logger{
		dir:      logDir,
		minLevel: LevelInfo,
	}

	if err := l.openLogFile(); err != nil {
		return nil, err
	}

	return l, nil
}

// openLogFile opens or rotates the log file based on date.
func (l *Logger) openLogFile() error {
	today := time.Now().Format("2006-01-02")

	if l.file != nil && l.currentDate == today {
		return nil // Already have correct file open
	}

	// Close existing file
	if l.file != nil {
		l.file.Close()
	}

	// Open new log file
	logPath := filepath.Join(l.dir, fmt.Sprintf("firebell-%s.log", today))
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	l.file = f
	l.currentDate = today

	// Update symlink to current log
	symlink := filepath.Join(l.dir, "firebell.log")
	os.Remove(symlink)
	os.Symlink(filepath.Base(logPath), symlink)

	return nil
}

// SetLevel sets the minimum log level.
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minLevel = level
}

// Log writes a log entry.
func (l *Logger) Log(level LogLevel, msg string, args ...interface{}) {
	if level < l.minLevel {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Check for date rotation
	if err := l.openLogFile(); err != nil {
		fmt.Fprintf(os.Stderr, "Logger error: %v\n", err)
		return
	}

	// Format message
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   msg,
	}

	// Write both formats: human-readable line + JSON
	l.writeEntry(entry)
}

// LogEvent writes an event log entry with additional context.
func (l *Logger) LogEvent(level LogLevel, agent, event, msg string, details string) {
	if level < l.minLevel {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Check for date rotation
	if err := l.openLogFile(); err != nil {
		fmt.Fprintf(os.Stderr, "Logger error: %v\n", err)
		return
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   msg,
		Agent:     agent,
		Event:     event,
		Details:   details,
	}

	l.writeEntry(entry)
}

// writeEntry writes a log entry in both human-readable and JSON format.
func (l *Logger) writeEntry(entry LogEntry) {
	if l.file == nil {
		return
	}

	// Human-readable format
	ts := entry.Timestamp.Format("2006-01-02 15:04:05")
	var line string
	if entry.Agent != "" {
		line = fmt.Sprintf("%s [%s] [%s] %s", ts, entry.Level, entry.Agent, entry.Message)
	} else {
		line = fmt.Sprintf("%s [%s] %s", ts, entry.Level, entry.Message)
	}

	// Write human-readable line
	fmt.Fprintln(l.file, line)

	// Write JSON on same line (prefixed with JSON:)
	jsonData, err := json.Marshal(entry)
	if err == nil {
		fmt.Fprintf(l.file, "  JSON: %s\n", jsonData)
	}
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, args ...interface{}) {
	l.Log(LevelDebug, msg, args...)
}

// Info logs an info message.
func (l *Logger) Info(msg string, args ...interface{}) {
	l.Log(LevelInfo, msg, args...)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, args ...interface{}) {
	l.Log(LevelWarn, msg, args...)
}

// Error logs an error message.
func (l *Logger) Error(msg string, args ...interface{}) {
	l.Log(LevelError, msg, args...)
}

// Close closes the logger.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// Writer returns an io.Writer that writes to the log at INFO level.
func (l *Logger) Writer() io.Writer {
	return &logWriter{logger: l, level: LevelInfo}
}

// logWriter adapts Logger to io.Writer.
type logWriter struct {
	logger *Logger
	level  LogLevel
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	w.logger.Log(w.level, string(p))
	return len(p), nil
}

// LogPath returns the current log file path.
func (l *Logger) LogPath() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Name()
	}
	return filepath.Join(l.dir, "firebell.log")
}

// Dir returns the log directory.
func (l *Logger) Dir() string {
	return l.dir
}
