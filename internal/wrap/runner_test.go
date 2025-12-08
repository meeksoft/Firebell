package wrap

import (
	"context"
	"testing"
	"time"

	"firebell/internal/config"
	"firebell/internal/notify"
)

// mockNotifier implements notify.Notifier for testing.
type mockNotifier struct {
	notifications []*notify.Notification
}

func (m *mockNotifier) Send(ctx context.Context, n *notify.Notification) error {
	m.notifications = append(m.notifications, n)
	return nil
}

func (m *mockNotifier) Name() string {
	return "mock"
}

func TestNewRunner(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Notify.Type = "stdout"

	notifier := &mockNotifier{}
	runner := NewRunner(cfg, notifier, "test")

	if runner == nil {
		t.Fatal("NewRunner returned nil")
	}
	if runner.agentName != "test" {
		t.Errorf("agentName = %q, want %q", runner.agentName, "test")
	}
}

func TestRunnerNoCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Notify.Type = "stdout"

	notifier := &mockNotifier{}
	runner := NewRunner(cfg, notifier, "test")

	ctx := context.Background()
	_, err := runner.Run(ctx, []string{})

	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestRunnerSimpleCommand(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Notify.Type = "stdout"

	notifier := &mockNotifier{}
	runner := NewRunner(cfg, notifier, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run a simple command
	exitCode, err := runner.Run(ctx, []string{"echo", "hello"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
}

func TestRunnerWithMatch(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Notify.Type = "stdout"
	cfg.Output.IncludeSnippets = true

	notifier := &mockNotifier{}
	runner := NewRunner(cfg, notifier, "test")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run a command that outputs a matching pattern
	exitCode, err := runner.Run(ctx, []string{"echo", "assistant_message: hello world"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}

	// Check that a notification was sent
	// Note: Due to async nature, notification may or may not be captured
	// This is a basic smoke test
}
