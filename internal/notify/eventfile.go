// Package notify provides notification delivery for firebell.
package notify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EventFileNotifier writes events to a JSONL file for external consumption.
type EventFileNotifier struct {
	path    string
	maxSize int64
	mu      sync.Mutex
	file    *os.File
}

// NewEventFileNotifier creates a new event file notifier.
// If path is empty, it defaults to ~/.firebell/events.jsonl.
// If maxSize is 0, it defaults to 10MB.
func NewEventFileNotifier(path string, maxSize int64) (*EventFileNotifier, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, ".firebell", "events.jsonl")
	}

	if maxSize == 0 {
		maxSize = 10 * 1024 * 1024 // 10MB
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return &EventFileNotifier{
		path:    path,
		maxSize: maxSize,
	}, nil
}

// Name returns the notifier type.
func (e *EventFileNotifier) Name() string {
	return "eventfile"
}

// Path returns the event file path.
func (e *EventFileNotifier) Path() string {
	return e.path
}

// Send writes a notification as a JSON event to the file.
func (e *EventFileNotifier) Send(ctx context.Context, n *Notification) error {
	eventType := DetermineEventType(n)
	event := NewEventFromNotification(n, eventType)
	return e.WriteEvent(event)
}

// WriteEvent writes an event directly to the file.
func (e *EventFileNotifier) WriteEvent(event *Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if rotation is needed
	if err := e.maybeRotate(); err != nil {
		return fmt.Errorf("failed to rotate event file: %w", err)
	}

	// Open file if not already open
	if e.file == nil {
		f, err := os.OpenFile(e.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("failed to open event file: %w", err)
		}
		e.file = f
	}

	// Serialize event
	data, err := event.JSONLine()
	if err != nil {
		return fmt.Errorf("failed to serialize event: %w", err)
	}

	// Write with newline
	if _, err := e.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	// Sync to ensure data is written
	return e.file.Sync()
}

// maybeRotate checks if the file needs rotation and rotates if necessary.
// Must be called with e.mu held.
func (e *EventFileNotifier) maybeRotate() error {
	info, err := os.Stat(e.path)
	if os.IsNotExist(err) {
		return nil // File doesn't exist yet, no rotation needed
	}
	if err != nil {
		return err
	}

	if info.Size() < e.maxSize {
		return nil // File is under limit
	}

	// Close current file if open
	if e.file != nil {
		e.file.Close()
		e.file = nil
	}

	// Rotate: rename current file with timestamp
	timestamp := time.Now().Format("2006-01-02-150405")
	rotatedPath := e.path + "." + timestamp
	if err := os.Rename(e.path, rotatedPath); err != nil {
		return fmt.Errorf("failed to rotate file: %w", err)
	}

	return nil
}

// Close closes the event file.
func (e *EventFileNotifier) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.file != nil {
		err := e.file.Close()
		e.file = nil
		return err
	}
	return nil
}

// EmitDaemonStart writes a daemon_start event.
func (e *EventFileNotifier) EmitDaemonStart() error {
	event := NewEvent(EventDaemonStart).
		WithAgent("firebell").
		WithMessage("Firebell daemon started")
	return e.WriteEvent(event)
}

// EmitDaemonStop writes a daemon_stop event.
func (e *EventFileNotifier) EmitDaemonStop() error {
	event := NewEvent(EventDaemonStop).
		WithAgent("firebell").
		WithMessage("Firebell daemon stopping")
	return e.WriteEvent(event)
}
