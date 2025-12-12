package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"firebell/internal/notify"
)

func TestSocketServer_CreateAndClose(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	server, err := NewSocketServer(sockPath)
	if err != nil {
		t.Fatalf("NewSocketServer failed: %v", err)
	}

	// Verify socket file exists
	if _, err := os.Stat(sockPath); err != nil {
		t.Errorf("Socket file not created: %v", err)
	}

	// Verify path
	if server.Path() != sockPath {
		t.Errorf("Path = %q, want %q", server.Path(), sockPath)
	}

	// Close server
	if err := server.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Verify socket file removed
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("Socket file should be removed after close")
	}
}

func TestSocketServer_DefaultPath(t *testing.T) {
	// Test with empty path (should use default)
	server, err := NewSocketServer("")
	if err != nil {
		t.Fatalf("NewSocketServer failed: %v", err)
	}
	defer server.Close()

	home, _ := os.UserHomeDir()
	expectedPath := filepath.Join(home, ".firebell", "firebell.sock")
	if server.Path() != expectedPath {
		t.Errorf("Path = %q, want %q", server.Path(), expectedPath)
	}
}

func TestSocketServer_AcceptConnection(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	server, err := NewSocketServer(sockPath)
	if err != nil {
		t.Fatalf("NewSocketServer failed: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server.Start(ctx)

	// Connect as client
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Read welcome message
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read welcome: %v", err)
	}

	var welcome map[string]string
	if err := json.Unmarshal([]byte(line), &welcome); err != nil {
		t.Fatalf("Failed to parse welcome: %v", err)
	}

	if welcome["type"] != "welcome" {
		t.Errorf("Welcome type = %q, want 'welcome'", welcome["type"])
	}

	// Verify client count
	time.Sleep(50 * time.Millisecond) // Allow time for registration
	if server.ClientCount() != 1 {
		t.Errorf("ClientCount = %d, want 1", server.ClientCount())
	}
}

func TestSocketServer_Broadcast(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	server, err := NewSocketServer(sockPath)
	if err != nil {
		t.Fatalf("NewSocketServer failed: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server.Start(ctx)

	// Connect two clients
	conn1, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to connect client 1: %v", err)
	}
	defer conn1.Close()

	conn2, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to connect client 2: %v", err)
	}
	defer conn2.Close()

	// Read welcome messages
	reader1 := bufio.NewReader(conn1)
	reader2 := bufio.NewReader(conn2)
	reader1.ReadString('\n')
	reader2.ReadString('\n')

	// Wait for clients to register
	time.Sleep(100 * time.Millisecond)

	// Broadcast an event
	event := notify.NewEvent(notify.EventCooling).
		WithAgent("Claude Code").
		WithMessage("No activity for 20 seconds")

	server.Broadcast(event)

	// Read from both clients
	line1, err := reader1.ReadString('\n')
	if err != nil {
		t.Fatalf("Client 1 failed to read: %v", err)
	}

	line2, err := reader2.ReadString('\n')
	if err != nil {
		t.Fatalf("Client 2 failed to read: %v", err)
	}

	// Verify both received the event
	var event1, event2 notify.Event
	json.Unmarshal([]byte(line1), &event1)
	json.Unmarshal([]byte(line2), &event2)

	if event1.Event != notify.EventCooling {
		t.Errorf("Client 1 event = %q, want %q", event1.Event, notify.EventCooling)
	}
	if event2.Event != notify.EventCooling {
		t.Errorf("Client 2 event = %q, want %q", event2.Event, notify.EventCooling)
	}
}

func TestSocketServer_ClientDisconnect(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	server, err := NewSocketServer(sockPath)
	if err != nil {
		t.Fatalf("NewSocketServer failed: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server.Start(ctx)

	// Connect client
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	// Wait for registration
	time.Sleep(100 * time.Millisecond)

	if server.ClientCount() != 1 {
		t.Errorf("ClientCount before disconnect = %d, want 1", server.ClientCount())
	}

	// Disconnect client
	conn.Close()

	// Broadcast to trigger cleanup
	event := notify.NewEvent(notify.EventActivity)
	server.Broadcast(event)

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)

	if server.ClientCount() != 0 {
		t.Errorf("ClientCount after disconnect = %d, want 0", server.ClientCount())
	}
}

func TestSocketNotifier_Send(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	server, err := NewSocketServer(sockPath)
	if err != nil {
		t.Fatalf("NewSocketServer failed: %v", err)
	}
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server.Start(ctx)

	// Create notifier
	notifier := NewSocketNotifier(server)

	if notifier.Name() != "socket" {
		t.Errorf("Name = %q, want 'socket'", notifier.Name())
	}

	// Connect client
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	reader.ReadString('\n') // Read welcome

	time.Sleep(50 * time.Millisecond)

	// Send notification
	notification := &notify.Notification{
		Title:   "Cooling",
		Agent:   "Test Agent",
		Message: "Test message",
		Time:    time.Now(),
	}

	err = notifier.Send(context.Background(), notification)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Read event
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read event: %v", err)
	}

	var event notify.Event
	json.Unmarshal([]byte(line), &event)

	if event.Event != notify.EventCooling {
		t.Errorf("Event type = %q, want %q", event.Event, notify.EventCooling)
	}
	if event.Agent != "Test Agent" {
		t.Errorf("Agent = %q, want 'Test Agent'", event.Agent)
	}
}
