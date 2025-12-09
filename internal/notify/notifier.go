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
func NewNotifier(cfg *config.Config) (Notifier, error) {
	switch cfg.Notify.Type {
	case "slack":
		if cfg.Notify.Slack.Webhook == "" {
			return nil, fmt.Errorf("slack webhook URL is required")
		}
		return NewSlackNotifier(cfg.Notify.Slack.Webhook), nil
	case "stdout":
		return NewStdoutNotifier(), nil
	default:
		return nil, fmt.Errorf("unknown notification type: %s", cfg.Notify.Type)
	}
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
