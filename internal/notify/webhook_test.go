package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"firebell/internal/config"
)

func TestWebhookNotifier_Send(t *testing.T) {
	var received atomic.Int32
	var lastEvent Event

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)

		// Verify headers
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}

		// Parse body
		if err := json.NewDecoder(r.Body).Decode(&lastEvent); err != nil {
			t.Errorf("Failed to decode body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create notifier
	notifier := NewWebhookNotifier([]config.WebhookConfig{
		{URL: server.URL},
	})

	// Send notification
	notification := &Notification{
		Title:   "Cooling",
		Agent:   "Claude Code",
		Message: "No activity for 20 seconds",
		Time:    time.Now(),
	}

	err := notifier.Send(context.Background(), notification)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Verify
	if received.Load() != 1 {
		t.Errorf("Received %d requests, want 1", received.Load())
	}
	if lastEvent.Event != EventCooling {
		t.Errorf("Event type = %q, want %q", lastEvent.Event, EventCooling)
	}
	if lastEvent.Agent != "Claude Code" {
		t.Errorf("Agent = %q, want %q", lastEvent.Agent, "Claude Code")
	}
}

func TestWebhookNotifier_CustomHeaders(t *testing.T) {
	var authHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier([]config.WebhookConfig{
		{
			URL: server.URL,
			Headers: map[string]string{
				"Authorization": "Bearer test-token",
			},
		},
	})

	notification := &Notification{
		Title: "Activity Detected",
		Agent: "Test",
		Time:  time.Now(),
	}

	err := notifier.Send(context.Background(), notification)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if authHeader != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want %q", authHeader, "Bearer test-token")
	}
}

func TestWebhookNotifier_EventFiltering(t *testing.T) {
	var received atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Only accept cooling events
	notifier := NewWebhookNotifier([]config.WebhookConfig{
		{
			URL:    server.URL,
			Events: []string{"cooling"},
		},
	})

	// Send activity event (should be filtered)
	activityNotification := &Notification{
		Title: "Activity Detected",
		Agent: "Test",
		Time:  time.Now(),
	}
	notifier.Send(context.Background(), activityNotification)

	if received.Load() != 0 {
		t.Errorf("Activity event should have been filtered, but received %d requests", received.Load())
	}

	// Send cooling event (should be sent)
	coolingNotification := &Notification{
		Title: "Cooling",
		Agent: "Test",
		Time:  time.Now(),
	}
	notifier.Send(context.Background(), coolingNotification)

	if received.Load() != 1 {
		t.Errorf("Cooling event should have been sent, received %d requests", received.Load())
	}
}

func TestWebhookNotifier_MultipleEndpoints(t *testing.T) {
	var received1, received2 atomic.Int32

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received1.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received2.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()

	notifier := NewWebhookNotifier([]config.WebhookConfig{
		{URL: server1.URL},
		{URL: server2.URL},
	})

	notification := &Notification{
		Title: "Cooling",
		Agent: "Test",
		Time:  time.Now(),
	}

	err := notifier.Send(context.Background(), notification)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if received1.Load() != 1 {
		t.Errorf("Server 1 received %d requests, want 1", received1.Load())
	}
	if received2.Load() != 1 {
		t.Errorf("Server 2 received %d requests, want 1", received2.Load())
	}
}

func TestWebhookNotifier_Retry(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier([]config.WebhookConfig{
		{URL: server.URL, Timeout: 1}, // 1 second timeout
	})

	notification := &Notification{
		Title: "Cooling",
		Agent: "Test",
		Time:  time.Now(),
	}

	// This should succeed on the third attempt
	err := notifier.Send(context.Background(), notification)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if attempts.Load() != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts.Load())
	}
}

func TestWebhookNotifier_AllEventsFilter(t *testing.T) {
	var received atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Use "all" filter
	notifier := NewWebhookNotifier([]config.WebhookConfig{
		{
			URL:    server.URL,
			Events: []string{"all"},
		},
	})

	// Both should be sent
	notifier.Send(context.Background(), &Notification{Title: "Activity Detected", Time: time.Now()})
	notifier.Send(context.Background(), &Notification{Title: "Cooling", Time: time.Now()})

	if received.Load() != 2 {
		t.Errorf("Expected 2 requests with 'all' filter, got %d", received.Load())
	}
}

func TestWebhookNotifier_EmptyConfig(t *testing.T) {
	notifier := NewWebhookNotifier([]config.WebhookConfig{})

	notification := &Notification{
		Title: "Cooling",
		Agent: "Test",
		Time:  time.Now(),
	}

	// Should not error with no endpoints
	err := notifier.Send(context.Background(), notification)
	if err != nil {
		t.Fatalf("Send with no endpoints should not error: %v", err)
	}
}

func TestWebhookNotifier_EndpointCount(t *testing.T) {
	notifier := NewWebhookNotifier([]config.WebhookConfig{
		{URL: "http://example.com/1"},
		{URL: "http://example.com/2"},
		{URL: ""}, // Empty URL should be skipped
	})

	if notifier.EndpointCount() != 2 {
		t.Errorf("EndpointCount = %d, want 2", notifier.EndpointCount())
	}
}

func TestTestWebhook(t *testing.T) {
	var received bool
	var eventType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		var event Event
		json.NewDecoder(r.Body).Decode(&event)
		eventType = string(event.Event)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := TestWebhook(context.Background(), server.URL, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("TestWebhook failed: %v", err)
	}

	if !received {
		t.Error("Test webhook was not received")
	}
	if eventType != "test" {
		t.Errorf("Event type = %q, want 'test'", eventType)
	}
}

func TestTestWebhook_WithHeaders(t *testing.T) {
	var authHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	headers := map[string]string{
		"X-Custom-Header": "custom-value",
	}

	err := TestWebhook(context.Background(), server.URL, headers, 5*time.Second)
	if err != nil {
		t.Fatalf("TestWebhook failed: %v", err)
	}

	if authHeader != "custom-value" {
		t.Errorf("Custom header = %q, want 'custom-value'", authHeader)
	}
}
