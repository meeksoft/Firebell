// Package notify provides notification delivery for firebell.
package notify

import (
	"context"
	"fmt"
	"strings"
)

// MultiNotifier sends notifications to multiple notifiers.
type MultiNotifier struct {
	primary   Notifier
	secondary []Notifier
}

// NewMultiNotifier creates a notifier that sends to multiple destinations.
// The primary notifier is required; secondary notifiers are optional.
func NewMultiNotifier(primary Notifier, secondary ...Notifier) *MultiNotifier {
	return &MultiNotifier{
		primary:   primary,
		secondary: secondary,
	}
}

// Name returns the combined notifier names.
func (m *MultiNotifier) Name() string {
	names := []string{m.primary.Name()}
	for _, n := range m.secondary {
		names = append(names, n.Name())
	}
	return strings.Join(names, "+")
}

// Send delivers the notification to all notifiers.
// Errors from secondary notifiers are logged but don't fail the operation.
func (m *MultiNotifier) Send(ctx context.Context, n *Notification) error {
	// Send to primary first
	if err := m.primary.Send(ctx, n); err != nil {
		return fmt.Errorf("primary notifier (%s) failed: %w", m.primary.Name(), err)
	}

	// Send to secondary notifiers (best effort)
	for _, notifier := range m.secondary {
		if err := notifier.Send(ctx, n); err != nil {
			// Log error but continue - secondary notifiers are best effort
			// In a real implementation, you might want to use a logger
			_ = err
		}
	}

	return nil
}

// Primary returns the primary notifier.
func (m *MultiNotifier) Primary() Notifier {
	return m.primary
}

// Secondary returns the secondary notifiers.
func (m *MultiNotifier) Secondary() []Notifier {
	return m.secondary
}

// Close closes all notifiers that implement io.Closer.
func (m *MultiNotifier) Close() error {
	var errs []error

	// Close primary if it has a Close method
	if closer, ok := m.primary.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	// Close secondary notifiers
	for _, n := range m.secondary {
		if closer, ok := n.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}
