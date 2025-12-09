package notify

import (
	"context"
	"fmt"
	"os"
	"time"
)

// StdoutNotifier prints notifications to stdout.
type StdoutNotifier struct{}

// NewStdoutNotifier creates a new stdout notifier.
func NewStdoutNotifier() *StdoutNotifier {
	return &StdoutNotifier{}
}

// Name returns the notifier type.
func (s *StdoutNotifier) Name() string {
	return "stdout"
}

// Send prints a notification to stdout.
func (s *StdoutNotifier) Send(ctx context.Context, n *Notification) error {
	timestamp := n.Time.Format("15:04:05")

	// Header line
	if n.Agent != "" {
		fmt.Fprintf(os.Stdout, "[%s] %s | %s\n", timestamp, n.Agent, n.Title)
	} else {
		fmt.Fprintf(os.Stdout, "[%s] %s\n", timestamp, n.Title)
	}

	// Message
	if n.Message != "" {
		fmt.Fprintf(os.Stdout, "  %s\n", n.Message)
	}

	// Snippet
	if n.Snippet != "" {
		fmt.Fprintln(os.Stdout, "  ---")
		lines := splitLines(n.Snippet)
		for _, line := range lines {
			fmt.Fprintf(os.Stdout, "  %s\n", line)
		}
		fmt.Fprintln(os.Stdout, "  ---")
	}

	fmt.Fprintln(os.Stdout)
	return nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// NewNotificationFromMatch creates a notification from a match event.
func NewNotificationFromMatch(agentName, displayName, reason, line string) *Notification {
	return &Notification{
		Title:   "Activity Detected",
		Agent:   displayName,
		Message: reason,
		Time:    time.Now(),
	}
}

// NewQuietNotification creates a "likely finished" notification.
func NewQuietNotification(displayName string, cpuPct float64) *Notification {
	msg := "No activity detected for quiet period"
	if cpuPct >= 0 {
		msg = fmt.Sprintf("No activity detected (CPU: %.1f%%)", cpuPct)
	}
	return &Notification{
		Title:   "Likely Finished",
		Agent:   displayName,
		Message: msg,
		Time:    time.Now(),
	}
}

// NewProcessExitNotification creates a process exit notification.
func NewProcessExitNotification(pid int) *Notification {
	return &Notification{
		Title:   "Process Exited",
		Agent:   "firebell",
		Message: fmt.Sprintf("Monitored process (PID %d) has terminated", pid),
		Time:    time.Now(),
	}
}
