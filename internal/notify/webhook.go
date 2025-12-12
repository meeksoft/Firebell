// Package notify provides notification delivery for firebell.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"firebell/internal/config"
)

// WebhookNotifier sends notifications to HTTP webhook endpoints.
type WebhookNotifier struct {
	webhooks []webhookEndpoint
	client   *http.Client
}

type webhookEndpoint struct {
	url     string
	events  map[string]bool // nil means all events
	headers map[string]string
	timeout time.Duration
}

// NewWebhookNotifier creates a notifier that sends to multiple webhook endpoints.
func NewWebhookNotifier(configs []config.WebhookConfig) *WebhookNotifier {
	endpoints := make([]webhookEndpoint, 0, len(configs))

	for _, cfg := range configs {
		if cfg.URL == "" {
			continue
		}

		endpoint := webhookEndpoint{
			url:     cfg.URL,
			headers: cfg.Headers,
			timeout: 10 * time.Second,
		}

		if cfg.Timeout > 0 {
			endpoint.timeout = time.Duration(cfg.Timeout) * time.Second
		}

		// Convert events list to map for fast lookup
		if len(cfg.Events) > 0 {
			endpoint.events = make(map[string]bool)
			for _, e := range cfg.Events {
				endpoint.events[e] = true
			}
		}

		endpoints = append(endpoints, endpoint)
	}

	return &WebhookNotifier{
		webhooks: endpoints,
		client: &http.Client{
			Timeout: 30 * time.Second, // Overall client timeout
		},
	}
}

// Name returns the notifier type.
func (w *WebhookNotifier) Name() string {
	return "webhook"
}

// Send delivers a notification to all configured webhooks.
func (w *WebhookNotifier) Send(ctx context.Context, n *Notification) error {
	if len(w.webhooks) == 0 {
		return nil
	}

	eventType := DetermineEventType(n)
	event := NewEventFromNotification(n, eventType)

	// Send to each webhook (best effort for each)
	var lastErr error
	for _, endpoint := range w.webhooks {
		// Check event filter
		if endpoint.events != nil && !endpoint.events[string(eventType)] && !endpoint.events["all"] {
			continue
		}

		if err := w.sendToEndpoint(ctx, endpoint, event); err != nil {
			lastErr = err
			// Continue to other endpoints even if one fails
		}
	}

	return lastErr
}

// sendToEndpoint sends an event to a single webhook endpoint with retry.
func (w *WebhookNotifier) sendToEndpoint(ctx context.Context, endpoint webhookEndpoint, event *Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Retry up to 3 times with exponential backoff
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s
			backoff := time.Duration(1<<attempt) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := w.doRequest(ctx, endpoint, data)
		if err == nil {
			return nil
		}
		lastErr = err

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return fmt.Errorf("webhook failed after 3 attempts: %w", lastErr)
}

// doRequest performs a single HTTP request to the webhook.
func (w *WebhookNotifier) doRequest(ctx context.Context, endpoint webhookEndpoint, data []byte) error {
	// Create context with endpoint-specific timeout
	reqCtx, cancel := context.WithTimeout(ctx, endpoint.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", endpoint.url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "firebell/1.1")

	// Add custom headers
	for k, v := range endpoint.headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// SendEvent sends an event directly to all configured webhooks.
func (w *WebhookNotifier) SendEvent(ctx context.Context, event *Event) error {
	if len(w.webhooks) == 0 {
		return nil
	}

	var lastErr error
	for _, endpoint := range w.webhooks {
		// Check event filter
		if endpoint.events != nil && !endpoint.events[string(event.Event)] && !endpoint.events["all"] {
			continue
		}

		if err := w.sendToEndpoint(ctx, endpoint, event); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// TestWebhook sends a test event to a specific URL and returns the result.
func TestWebhook(ctx context.Context, url string, headers map[string]string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	event := &Event{
		Event:     "test",
		Timestamp: time.Now(),
		Agent:     "firebell",
		Title:     "Test Notification",
		Message:   "Webhook configuration is working!",
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "firebell/1.1")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// EndpointCount returns the number of configured webhook endpoints.
func (w *WebhookNotifier) EndpointCount() int {
	return len(w.webhooks)
}
