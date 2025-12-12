// Package daemon provides daemon lifecycle management for firebell.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"firebell/internal/notify"
)

// SocketServer manages a Unix domain socket for external integrations.
type SocketServer struct {
	path     string
	listener net.Listener
	clients  map[net.Conn]bool
	mu       sync.RWMutex
	done     chan struct{}
}

// NewSocketServer creates a new socket server.
// If path is empty, it defaults to ~/.firebell/firebell.sock.
func NewSocketServer(path string) (*SocketServer, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, ".firebell", "firebell.sock")
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Remove existing socket file
	os.Remove(path)

	// Create listener
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket: %w", err)
	}

	// Set permissions (user only)
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("failed to set socket permissions: %w", err)
	}

	return &SocketServer{
		path:     path,
		listener: listener,
		clients:  make(map[net.Conn]bool),
		done:     make(chan struct{}),
	}, nil
}

// Path returns the socket path.
func (s *SocketServer) Path() string {
	return s.path
}

// Start begins accepting connections in a goroutine.
func (s *SocketServer) Start(ctx context.Context) {
	go s.acceptLoop(ctx)
}

// acceptLoop accepts new connections until context is cancelled.
func (s *SocketServer) acceptLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		// Set deadline to allow periodic context checks
		s.listener.(*net.UnixListener).SetDeadline(time.Now().Add(1 * time.Second))

		conn, err := s.listener.Accept()
		if err != nil {
			// Check if it's a timeout (expected)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			// Check if server is closing
			select {
			case <-s.done:
				return
			default:
			}
			continue
		}

		s.mu.Lock()
		s.clients[conn] = true
		s.mu.Unlock()

		// Handle client in goroutine
		go s.handleClient(conn)
	}
}

// handleClient manages a single client connection.
func (s *SocketServer) handleClient(conn net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		conn.Close()
	}()

	// Send welcome message
	welcome := map[string]string{
		"type":    "welcome",
		"message": "Connected to firebell socket",
	}
	data, _ := json.Marshal(welcome)
	conn.Write(append(data, '\n'))

	// Keep connection alive until closed
	buf := make([]byte, 1024)
	for {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, err := conn.Read(buf)
		if err != nil {
			return
		}
	}
}

// Broadcast sends an event to all connected clients.
func (s *SocketServer) Broadcast(event *notify.Event) {
	data, err := event.JSON()
	if err != nil {
		return
	}
	data = append(data, '\n')

	s.mu.RLock()
	clients := make([]net.Conn, 0, len(s.clients))
	for conn := range s.clients {
		clients = append(clients, conn)
	}
	s.mu.RUnlock()

	for _, conn := range clients {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err := conn.Write(data)
		if err != nil {
			// Remove failed client
			s.mu.Lock()
			delete(s.clients, conn)
			s.mu.Unlock()
			conn.Close()
		}
	}
}

// ClientCount returns the number of connected clients.
func (s *SocketServer) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// Close shuts down the socket server.
func (s *SocketServer) Close() error {
	close(s.done)

	// Close all client connections
	s.mu.Lock()
	for conn := range s.clients {
		conn.Close()
	}
	s.clients = make(map[net.Conn]bool)
	s.mu.Unlock()

	// Close listener
	if s.listener != nil {
		s.listener.Close()
	}

	// Remove socket file
	os.Remove(s.path)

	return nil
}

// SocketNotifier wraps a SocketServer to implement the Notifier interface.
type SocketNotifier struct {
	server *SocketServer
}

// NewSocketNotifier creates a notifier that broadcasts to socket clients.
func NewSocketNotifier(server *SocketServer) *SocketNotifier {
	return &SocketNotifier{server: server}
}

// Name returns the notifier type.
func (s *SocketNotifier) Name() string {
	return "socket"
}

// Send broadcasts a notification to all connected clients.
func (s *SocketNotifier) Send(ctx context.Context, n *notify.Notification) error {
	eventType := notify.DetermineEventType(n)
	event := notify.NewEventFromNotification(n, eventType)
	s.server.Broadcast(event)
	return nil
}

// Close closes the underlying socket server.
func (s *SocketNotifier) Close() error {
	return s.server.Close()
}

// Server returns the underlying socket server.
func (s *SocketNotifier) Server() *SocketServer {
	return s.server
}
