package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewLock(t *testing.T) {
	dir := t.TempDir()
	lock := NewLock(dir)

	if lock == nil {
		t.Fatal("NewLock returned nil")
	}
	if lock.Path() != filepath.Join(dir, "firebell.lock") {
		t.Errorf("Lock path = %q, want %q", lock.Path(), filepath.Join(dir, "firebell.lock"))
	}
}

func TestLockTryLock(t *testing.T) {
	dir := t.TempDir()
	lock := NewLock(dir)

	// First lock should succeed
	if err := lock.TryLock(); err != nil {
		t.Fatalf("TryLock failed: %v", err)
	}

	// Verify PID was written
	data, err := os.ReadFile(lock.Path())
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	if len(data) == 0 {
		t.Error("Lock file is empty")
	}

	// Unlock
	if err := lock.Unlock(); err != nil {
		t.Errorf("Unlock failed: %v", err)
	}
}

func TestLockIsRunning(t *testing.T) {
	dir := t.TempDir()
	lock := NewLock(dir)

	// Not running initially
	running, _ := lock.IsRunning()
	if running {
		t.Error("IsRunning returned true when no lock held")
	}

	// Acquire lock
	if err := lock.TryLock(); err != nil {
		t.Fatalf("TryLock failed: %v", err)
	}
	defer lock.Unlock()

	// Now should be running (same process check)
	lock2 := NewLock(dir)
	running, pid := lock2.IsRunning()
	if !running {
		t.Error("IsRunning returned false when lock held")
	}
	if pid != os.Getpid() {
		t.Errorf("PID = %d, want %d", pid, os.Getpid())
	}
}

func TestNewDaemon(t *testing.T) {
	dir := t.TempDir()
	d := NewDaemon(dir)

	if d == nil {
		t.Fatal("NewDaemon returned nil")
	}
	if d.Dir() != dir {
		t.Errorf("Dir = %q, want %q", d.Dir(), dir)
	}
}

func TestDaemonStatus(t *testing.T) {
	dir := t.TempDir()
	d := NewDaemon(dir)

	// Should not be running
	running, pid, uptime := d.Status()
	if running {
		t.Error("Status shows running when daemon not started")
	}
	if pid != 0 {
		t.Errorf("PID = %d, want 0", pid)
	}
	if uptime != 0 {
		t.Errorf("Uptime = %v, want 0", uptime)
	}
}

func TestNewLogger(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewLogger(dir)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	// Log directory should be created
	logDir := filepath.Join(dir, "logs")
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Error("Log directory not created")
	}
}

func TestLoggerWrite(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewLogger(dir)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}

	// Write some logs
	logger.Info("Test message")
	logger.Warn("Warning message")
	logger.Error("Error message")
	logger.Close()

	// Read log file
	logPath := logger.LogPath()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	content := string(data)
	if len(content) == 0 {
		t.Error("Log file is empty")
	}

	// Check content contains our messages
	if !contains(content, "Test message") {
		t.Error("Log missing 'Test message'")
	}
	if !contains(content, "Warning message") {
		t.Error("Log missing 'Warning message'")
	}
	if !contains(content, "Error message") {
		t.Error("Log missing 'Error message'")
	}
}

func TestLoggerEvent(t *testing.T) {
	dir := t.TempDir()
	logger, err := NewLogger(dir)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}

	logger.LogEvent(LevelInfo, "claude", "activity", "AI activity detected", "details here")
	logger.Close()

	data, err := os.ReadFile(logger.LogPath())
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	content := string(data)
	if !contains(content, "claude") {
		t.Error("Log missing agent name")
	}
	if !contains(content, "AI activity detected") {
		t.Error("Log missing message")
	}
}

func TestCleanupLogs(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0755)

	// Create old log files
	oldDate := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	oldLog := filepath.Join(logDir, "firebell-"+oldDate+".log")
	os.WriteFile(oldLog, []byte("old log"), 0644)

	// Create recent log file
	recentDate := time.Now().Format("2006-01-02")
	recentLog := filepath.Join(logDir, "firebell-"+recentDate+".log")
	os.WriteFile(recentLog, []byte("recent log"), 0644)

	// Cleanup with 7 day retention
	deleted, err := CleanupLogs(logDir, 7)
	if err != nil {
		t.Fatalf("CleanupLogs failed: %v", err)
	}

	if deleted != 1 {
		t.Errorf("Deleted = %d, want 1", deleted)
	}

	// Old log should be deleted
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Error("Old log file was not deleted")
	}

	// Recent log should exist
	if _, err := os.Stat(recentLog); os.IsNotExist(err) {
		t.Error("Recent log file was incorrectly deleted")
	}
}

func TestCleanupLogsZeroRetention(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0755)

	// Create old log file
	oldDate := time.Now().AddDate(0, 0, -100).Format("2006-01-02")
	oldLog := filepath.Join(logDir, "firebell-"+oldDate+".log")
	os.WriteFile(oldLog, []byte("old log"), 0644)

	// Cleanup with 0 day retention (keep forever)
	deleted, err := CleanupLogs(logDir, 0)
	if err != nil {
		t.Fatalf("CleanupLogs failed: %v", err)
	}

	if deleted != 0 {
		t.Errorf("Deleted = %d, want 0 (retention=0 means keep forever)", deleted)
	}

	// Old log should still exist
	if _, err := os.Stat(oldLog); os.IsNotExist(err) {
		t.Error("Log file was incorrectly deleted with retention=0")
	}
}

func TestGetLogFiles(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	os.MkdirAll(logDir, 0755)

	// Create log files
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	os.WriteFile(filepath.Join(logDir, "firebell-"+today+".log"), []byte("today"), 0644)
	os.WriteFile(filepath.Join(logDir, "firebell-"+yesterday+".log"), []byte("yesterday"), 0644)
	os.WriteFile(filepath.Join(logDir, "other.txt"), []byte("other"), 0644) // Should be ignored

	logs, err := GetLogFiles(logDir)
	if err != nil {
		t.Fatalf("GetLogFiles failed: %v", err)
	}

	if len(logs) != 2 {
		t.Errorf("Got %d logs, want 2", len(logs))
	}

	// Should be sorted newest first
	if len(logs) >= 2 {
		if logs[0].Date.Before(logs[1].Date) {
			t.Error("Logs not sorted newest first")
		}
	}
}

func TestIsDaemon(t *testing.T) {
	// Not set
	os.Unsetenv(DaemonEnvVar)
	if IsDaemon() {
		t.Error("IsDaemon returned true when env not set")
	}

	// Set to 1
	os.Setenv(DaemonEnvVar, "1")
	if !IsDaemon() {
		t.Error("IsDaemon returned false when env set to 1")
	}

	// Set to something else
	os.Setenv(DaemonEnvVar, "0")
	if IsDaemon() {
		t.Error("IsDaemon returned true when env set to 0")
	}

	// Cleanup
	os.Unsetenv(DaemonEnvVar)
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
