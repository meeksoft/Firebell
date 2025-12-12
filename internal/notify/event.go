// Package notify provides notification delivery for firebell.
package notify

import (
	"encoding/json"
	"time"
)

// EventType represents the type of event being notified.
type EventType string

const (
	EventActivity          EventType = "activity"
	EventCooling           EventType = "cooling"
	EventAwaiting EventType = "awaiting" // Waiting for user input (inferred)
	EventHolding  EventType = "holding"  // Waiting for tool approval (immediate)
	EventProcessExit       EventType = "process_exit"
	EventDaemonStart       EventType = "daemon_start"
	EventDaemonStop        EventType = "daemon_stop"
)

// Event is the unified event structure used by all hook/integration methods.
// This provides a consistent JSON schema across webhooks, event files, and sockets.
type Event struct {
	Event     EventType         `json:"event"`
	Timestamp time.Time         `json:"timestamp"`
	Agent     string            `json:"agent,omitempty"`
	Title     string            `json:"title,omitempty"`
	Message   string            `json:"message,omitempty"`
	Snippet   string            `json:"snippet,omitempty"`
	Metadata  map[string]any    `json:"metadata,omitempty"`
}

// NewEvent creates a new Event with the current timestamp.
func NewEvent(eventType EventType) *Event {
	return &Event{
		Event:     eventType,
		Timestamp: time.Now(),
	}
}

// NewEventFromNotification converts a Notification to an Event.
func NewEventFromNotification(n *Notification, eventType EventType) *Event {
	return &Event{
		Event:     eventType,
		Timestamp: n.Time,
		Agent:     n.Agent,
		Title:     n.Title,
		Message:   n.Message,
		Snippet:   n.Snippet,
	}
}

// WithAgent sets the agent name and returns the event for chaining.
func (e *Event) WithAgent(agent string) *Event {
	e.Agent = agent
	return e
}

// WithMessage sets the message and returns the event for chaining.
func (e *Event) WithMessage(message string) *Event {
	e.Message = message
	return e
}

// WithMetadata adds metadata key-value pairs and returns the event for chaining.
func (e *Event) WithMetadata(key string, value any) *Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]any)
	}
	e.Metadata[key] = value
	return e
}

// JSON returns the event serialized as JSON bytes.
func (e *Event) JSON() ([]byte, error) {
	return json.Marshal(e)
}

// JSONLine returns the event as a JSON line (no trailing newline).
func (e *Event) JSONLine() ([]byte, error) {
	return json.Marshal(e)
}

// DetermineEventType infers the event type from a Notification.
func DetermineEventType(n *Notification) EventType {
	switch n.Title {
	case "Cooling":
		return EventCooling
	case "Awaiting":
		return EventAwaiting
	case "Holding":
		return EventHolding
	case "Process Exited", "Process Exit":
		return EventProcessExit
	default:
		return EventActivity
	}
}
