package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CleanupLogs removes log files older than the specified retention period.
// retentionDays of 0 means keep forever.
func CleanupLogs(logDir string, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil // Keep forever
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	deleted := 0

	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read log directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Skip symlinks and non-log files
		if name == "firebell.log" {
			continue
		}

		// Only process firebell-*.log files
		if !strings.HasPrefix(name, "firebell-") || !strings.HasSuffix(name, ".log") {
			continue
		}

		// Parse date from filename: firebell-2006-01-02.log
		dateStr := strings.TrimPrefix(name, "firebell-")
		dateStr = strings.TrimSuffix(dateStr, ".log")

		logDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			// Can't parse date, skip
			continue
		}

		// Check if older than retention period
		if logDate.Before(cutoff) {
			logPath := filepath.Join(logDir, name)
			if err := os.Remove(logPath); err != nil {
				// Log error but continue
				fmt.Fprintf(os.Stderr, "Warning: failed to remove old log %s: %v\n", name, err)
				continue
			}
			deleted++
		}
	}

	return deleted, nil
}

// CleanupOnStart runs cleanup when daemon starts.
func CleanupOnStart(dir string, retentionDays int) {
	logDir := filepath.Join(dir, "logs")
	deleted, err := CleanupLogs(logDir, retentionDays)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: log cleanup failed: %v\n", err)
		return
	}
	if deleted > 0 {
		fmt.Printf("Cleaned up %d old log file(s)\n", deleted)
	}
}

// GetLogFiles returns a list of log files sorted by date (newest first).
func GetLogFiles(logDir string) ([]LogFileInfo, error) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var logs []LogFileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == "firebell.log" || !strings.HasPrefix(name, "firebell-") || !strings.HasSuffix(name, ".log") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Parse date from filename
		dateStr := strings.TrimPrefix(name, "firebell-")
		dateStr = strings.TrimSuffix(dateStr, ".log")
		logDate, _ := time.Parse("2006-01-02", dateStr)

		logs = append(logs, LogFileInfo{
			Name:    name,
			Path:    filepath.Join(logDir, name),
			Date:    logDate,
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	// Sort by date descending (newest first)
	for i := 0; i < len(logs)-1; i++ {
		for j := i + 1; j < len(logs); j++ {
			if logs[j].Date.After(logs[i].Date) {
				logs[i], logs[j] = logs[j], logs[i]
			}
		}
	}

	return logs, nil
}

// LogFileInfo holds information about a log file.
type LogFileInfo struct {
	Name    string
	Path    string
	Date    time.Time
	Size    int64
	ModTime time.Time
}

// TotalLogSize returns the total size of all log files.
func TotalLogSize(logDir string) (int64, error) {
	logs, err := GetLogFiles(logDir)
	if err != nil {
		return 0, err
	}

	var total int64
	for _, log := range logs {
		total += log.Size
	}
	return total, nil
}
