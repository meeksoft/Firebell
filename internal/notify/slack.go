package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SlackNotifier sends notifications via Slack Incoming Webhooks.
type SlackNotifier struct {
	webhook string
	client  *http.Client
}

// NewSlackNotifier creates a new Slack notifier.
func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhook: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the notifier type.
func (s *SlackNotifier) Name() string {
	return "slack"
}

// Send delivers a notification to Slack.
func (s *SlackNotifier) Send(ctx context.Context, n *Notification) error {
	// Build message body
	body := FormatNotification(n, "normal", true)

	// Create Slack payload
	payload := map[string]string{"text": body}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", s.webhook, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}

	return nil
}

// TestWebhook sends a test message to verify the webhook works.
func (s *SlackNotifier) TestWebhook(ctx context.Context) error {
	n := &Notification{
		Title:   "Test Notification",
		Agent:   "firebell",
		Message: "Webhook configuration is working!",
		Time:    time.Now(),
	}
	return s.Send(ctx, n)
}
