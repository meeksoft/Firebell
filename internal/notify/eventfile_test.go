package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEventFileNotifier_Send(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	eventPath := filepath.Join(tmpDir, "events.jsonl")

	// Create notifier
	notifier, err := NewEventFileNotifier(eventPath, 0)
	if err != nil {
		t.Fatalf("NewEventFileNotifier failed: %v", err)
	}
	defer notifier.Close()

	// Send a notification
	notification := &Notification{
		Title:   "Cooling",
		Agent:   "Claude Code",
		Message: "No activity for 20 seconds",
		Snippet: "test snippet",
		Time:    time.Now(),
	}

	if err := notifier.Send(context.Background(), notification); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Read and verify the file
	data, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatalf("Failed to read event file: %v", err)
	}

	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	if event.Event != EventCooling {
		t.Errorf("Event type = %q, want %q", event.Event, EventCooling)
	}
	if event.Agent != "Claude Code" {
		t.Errorf("Agent = %q, want %q", event.Agent, "Claude Code")
	}
	if event.Message != "No activity for 20 seconds" {
		t.Errorf("Message = %q, want %q", event.Message, "No activity for 20 seconds")
	}
}

func TestEventFileNotifier_WriteEvent(t *testing.T) {
	tmpDir := t.TempDir()
	eventPath := filepath.Join(tmpDir, "events.jsonl")

	notifier, err := NewEventFileNotifier(eventPath, 0)
	if err != nil {
		t.Fatalf("NewEventFileNotifier failed: %v", err)
	}
	defer notifier.Close()

	// Write multiple events
	events := []*Event{
		NewEvent(EventDaemonStart).WithAgent("firebell").WithMessage("Started"),
		NewEvent(EventActivity).WithAgent("Claude Code").WithMessage("Activity detected"),
		NewEvent(EventCooling).WithAgent("Claude Code").WithMessage("Cooling"),
	}

	for _, e := range events {
		if err := notifier.WriteEvent(e); err != nil {
			t.Fatalf("WriteEvent failed: %v", err)
		}
	}

	// Read and verify
	file, err := os.Open(eventPath)
	if err != nil {
		t.Fatalf("Failed to open event file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	i := 0
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("Failed to unmarshal line %d: %v", i, err)
		}
		if event.Event != events[i].Event {
			t.Errorf("Line %d: event type = %q, want %q", i, event.Event, events[i].Event)
		}
		i++
	}

	if i != len(events) {
		t.Errorf("Read %d events, want %d", i, len(events))
	}
}

func TestEventFileNotifier_Rotation(t *testing.T) {
	tmpDir := t.TempDir()
	eventPath := filepath.Join(tmpDir, "events.jsonl")

	// Create notifier with small max size (500 bytes)
	notifier, err := NewEventFileNotifier(eventPath, 500)
	if err != nil {
		t.Fatalf("NewEventFileNotifier failed: %v", err)
	}
	defer notifier.Close()

	// Write events until rotation should occur
	for i := 0; i < 20; i++ {
		event := NewEvent(EventActivity).
			WithAgent("Test Agent").
			WithMessage("This is a test message that should fill up the file quickly")
		if err := notifier.WriteEvent(event); err != nil {
			t.Fatalf("WriteEvent failed on iteration %d: %v", i, err)
		}
	}

	// Check that rotation file exists
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read temp dir: %v", err)
	}

	rotatedFiles := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "events.jsonl.") {
			rotatedFiles++
		}
	}

	if rotatedFiles == 0 {
		t.Error("Expected at least one rotated file, found none")
	}
}

func TestEventFileNotifier_DefaultPath(t *testing.T) {
	// Test with empty path (should use default)
	notifier, err := NewEventFileNotifier("", 0)
	if err != nil {
		t.Fatalf("NewEventFileNotifier failed: %v", err)
	}

	home, _ := os.UserHomeDir()
	expectedPath := filepath.Join(home, ".firebell", "events.jsonl")
	if notifier.Path() != expectedPath {
		t.Errorf("Path = %q, want %q", notifier.Path(), expectedPath)
	}
}

func TestEventFileNotifier_DaemonEvents(t *testing.T) {
	tmpDir := t.TempDir()
	eventPath := filepath.Join(tmpDir, "events.jsonl")

	notifier, err := NewEventFileNotifier(eventPath, 0)
	if err != nil {
		t.Fatalf("NewEventFileNotifier failed: %v", err)
	}

	// Emit daemon start
	if err := notifier.EmitDaemonStart(); err != nil {
		t.Fatalf("EmitDaemonStart failed: %v", err)
	}

	// Emit daemon stop
	if err := notifier.EmitDaemonStop(); err != nil {
		t.Fatalf("EmitDaemonStop failed: %v", err)
	}

	notifier.Close()

	// Read and verify
	file, err := os.Open(eventPath)
	if err != nil {
		t.Fatalf("Failed to open event file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// First line: daemon_start
	if !scanner.Scan() {
		t.Fatal("Expected first line")
	}
	var startEvent Event
	if err := json.Unmarshal(scanner.Bytes(), &startEvent); err != nil {
		t.Fatalf("Failed to unmarshal start event: %v", err)
	}
	if startEvent.Event != EventDaemonStart {
		t.Errorf("First event = %q, want %q", startEvent.Event, EventDaemonStart)
	}

	// Second line: daemon_stop
	if !scanner.Scan() {
		t.Fatal("Expected second line")
	}
	var stopEvent Event
	if err := json.Unmarshal(scanner.Bytes(), &stopEvent); err != nil {
		t.Fatalf("Failed to unmarshal stop event: %v", err)
	}
	if stopEvent.Event != EventDaemonStop {
		t.Errorf("Second event = %q, want %q", stopEvent.Event, EventDaemonStop)
	}
}

func TestDetermineEventType(t *testing.T) {
	tests := []struct {
		title    string
		expected EventType
	}{
		{"Cooling", EventCooling},
		{"Process Exited", EventProcessExit},
		{"Process Exit", EventProcessExit},
		{"Activity Detected", EventActivity},
		{"Something Else", EventActivity},
		{"", EventActivity},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			n := &Notification{Title: tt.title}
			got := DetermineEventType(n)
			if got != tt.expected {
				t.Errorf("DetermineEventType(%q) = %q, want %q", tt.title, got, tt.expected)
			}
		})
	}
}

func TestEvent_Chaining(t *testing.T) {
	event := NewEvent(EventActivity).
		WithAgent("Test Agent").
		WithMessage("Test message").
		WithMetadata("cpu_percent", 5.5).
		WithMetadata("pid", 12345)

	if event.Event != EventActivity {
		t.Errorf("Event = %q, want %q", event.Event, EventActivity)
	}
	if event.Agent != "Test Agent" {
		t.Errorf("Agent = %q, want %q", event.Agent, "Test Agent")
	}
	if event.Message != "Test message" {
		t.Errorf("Message = %q, want %q", event.Message, "Test message")
	}
	if event.Metadata["cpu_percent"] != 5.5 {
		t.Errorf("Metadata[cpu_percent] = %v, want 5.5", event.Metadata["cpu_percent"])
	}
	if event.Metadata["pid"] != 12345 {
		t.Errorf("Metadata[pid] = %v, want 12345", event.Metadata["pid"])
	}
}

func TestEvent_JSON(t *testing.T) {
	event := NewEvent(EventCooling).
		WithAgent("Claude Code").
		WithMessage("No activity")

	data, err := event.JSON()
	if err != nil {
		t.Fatalf("JSON() failed: %v", err)
	}

	// Parse back
	var parsed Event
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if parsed.Event != EventCooling {
		t.Errorf("Event = %q, want %q", parsed.Event, EventCooling)
	}
	if parsed.Agent != "Claude Code" {
		t.Errorf("Agent = %q, want %q", parsed.Agent, "Claude Code")
	}
}
