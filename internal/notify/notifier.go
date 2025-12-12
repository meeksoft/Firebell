// Package notify provides notification delivery for firebell.
package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"firebell/internal/config"
)

// Notification represents a message to be sent.
type Notification struct {
	Title   string    // Main title/header (e.g., "Activity Detected")
	Agent   string    // Agent name (e.g., "Claude Code")
	Message string    // Body text
	Snippet string    // Optional log context
	Time    time.Time // When this notification was created
}

// Notifier is the interface for sending notifications.
type Notifier interface {
	// Send delivers a notification.
	Send(ctx context.Context, n *Notification) error

	// Name returns the notifier type name.
	Name() string
}

// NewNotifier creates the appropriate notifier based on config.
// If event file or webhooks are enabled, returns a MultiNotifier that sends
// to the primary notifier plus any secondary notifiers.
func NewNotifier(cfg *config.Config) (Notifier, error) {
	return NewNotifierWithExtras(cfg, nil)
}

// NewNotifierWithExtras creates a notifier with optional extra secondary notifiers.
// This allows adding notifiers that aren't created from config (like socket notifier).
func NewNotifierWithExtras(cfg *config.Config, extras []Notifier) (Notifier, error) {
	// Create primary notifier
	var primary Notifier
	switch cfg.Notify.Type {
	case "slack":
		if cfg.Notify.Slack.Webhook == "" {
			return nil, fmt.Errorf("slack webhook URL is required")
		}
		primary = NewSlackNotifier(cfg.Notify.Slack.Webhook)
	case "stdout":
		primary = NewStdoutNotifier()
	default:
		return nil, fmt.Errorf("unknown notification type: %s", cfg.Notify.Type)
	}

	// Collect secondary notifiers
	var secondary []Notifier

	// Add event file notifier if enabled
	if cfg.Daemon.EventFile {
		eventFile, err := NewEventFileNotifier(cfg.Daemon.EventFilePath, cfg.Daemon.EventFileMaxSize)
		if err == nil {
			secondary = append(secondary, eventFile)
		}
		// Log warning but continue without event file if it fails
	}

	// Add webhook notifiers if configured
	if len(cfg.Notify.Webhooks) > 0 {
		webhookNotifier := NewWebhookNotifier(cfg.Notify.Webhooks)
		if webhookNotifier.EndpointCount() > 0 {
			secondary = append(secondary, webhookNotifier)
		}
	}

	// Add extra notifiers (like socket)
	secondary = append(secondary, extras...)

	// Return multi-notifier if we have secondary notifiers
	if len(secondary) > 0 {
		return NewMultiNotifier(primary, secondary...), nil
	}

	return primary, nil
}

// FormatNotification formats a notification for display.
func FormatNotification(n *Notification, verbosity string, includeSnippet bool) string {
	var sb strings.Builder

	// Title with agent
	if n.Agent != "" {
		sb.WriteString(fmt.Sprintf("*%s* | %s\n", n.Agent, n.Title))
	} else {
		sb.WriteString(fmt.Sprintf("*%s*\n", n.Title))
	}

	// Message based on verbosity
	switch verbosity {
	case "minimal":
		// Just title
	case "verbose":
		// Full message
		if n.Message != "" {
			sb.WriteString(n.Message)
			sb.WriteString("\n")
		}
		if includeSnippet && n.Snippet != "" {
			sb.WriteString("```\n")
			sb.WriteString(n.Snippet)
			sb.WriteString("\n```")
		}
	default: // "normal"
		if n.Message != "" {
			sb.WriteString(n.Message)
			sb.WriteString("\n")
		}
		if includeSnippet && n.Snippet != "" {
			sb.WriteString("```\n")
			sb.WriteString(truncate(n.Snippet, 500))
			sb.WriteString("\n```")
		}
	}

	return sb.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen > 3 {
		return s[:maxLen-3] + "..."
	}
	return s[:maxLen]
}
