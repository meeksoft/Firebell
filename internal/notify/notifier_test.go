package notify

import (
	"testing"
	"time"
)

func TestFormatNotification(t *testing.T) {
	n := &Notification{
		Title:   "Activity Detected",
		Agent:   "Claude Code",
		Message: "Test message",
		Snippet: "some log content",
		Time:    time.Now(),
	}

	t.Run("minimal verbosity", func(t *testing.T) {
		result := FormatNotification(n, "minimal", true)

		// Should contain title and agent
		if !containsSubstr(result, "Claude Code") {
			t.Error("missing agent in minimal output")
		}
		if !containsSubstr(result, "Activity Detected") {
			t.Error("missing title in minimal output")
		}
		// Should not contain message in minimal
		if containsSubstr(result, "Test message") {
			t.Error("minimal should not include message")
		}
	})

	t.Run("normal verbosity", func(t *testing.T) {
		result := FormatNotification(n, "normal", true)

		if !containsSubstr(result, "Claude Code") {
			t.Error("missing agent")
		}
		if !containsSubstr(result, "Test message") {
			t.Error("missing message in normal output")
		}
		if !containsSubstr(result, "some log content") {
			t.Error("missing snippet in normal output")
		}
	})

	t.Run("normal without snippet", func(t *testing.T) {
		result := FormatNotification(n, "normal", false)

		if containsSubstr(result, "some log content") {
			t.Error("snippet should be excluded when includeSnippet=false")
		}
	})

	t.Run("verbose verbosity", func(t *testing.T) {
		result := FormatNotification(n, "verbose", true)

		if !containsSubstr(result, "Claude Code") {
			t.Error("missing agent")
		}
		if !containsSubstr(result, "Test message") {
			t.Error("missing message")
		}
		if !containsSubstr(result, "some log content") {
			t.Error("missing snippet")
		}
	})

	t.Run("no agent", func(t *testing.T) {
		noAgent := &Notification{
			Title:   "Test",
			Message: "msg",
			Time:    time.Now(),
		}
		result := FormatNotification(noAgent, "normal", false)
		if !containsSubstr(result, "*Test*") {
			t.Error("should format title without agent")
		}
	})
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is longer", 10, "this is..."},
		{"ab", 3, "ab"},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestNewNotificationFromMatch(t *testing.T) {
	n := NewNotificationFromMatch("claude", "Claude Code", "assistant response", "test line")

	if n.Title != "Activity Detected" {
		t.Errorf("Title = %q, want 'Activity Detected'", n.Title)
	}
	if n.Agent != "Claude Code" {
		t.Errorf("Agent = %q, want 'Claude Code'", n.Agent)
	}
	if n.Message != "assistant response" {
		t.Errorf("Message = %q, want 'assistant response'", n.Message)
	}
	if n.Time.IsZero() {
		t.Error("Time should be set")
	}
}

func TestNewQuietNotification(t *testing.T) {
	t.Run("without CPU", func(t *testing.T) {
		n := NewQuietNotification("Claude Code", -1)
		if n.Title != "Cooling" {
			t.Errorf("Title = %q, want 'Cooling'", n.Title)
		}
		if containsSubstr(n.Message, "CPU") {
			t.Error("should not contain CPU when cpuPct < 0")
		}
	})

	t.Run("with CPU", func(t *testing.T) {
		n := NewQuietNotification("Claude Code", 5.5)
		if !containsSubstr(n.Message, "CPU") {
			t.Error("should contain CPU percentage")
		}
		if !containsSubstr(n.Message, "5.5%") {
			t.Error("should contain formatted CPU value")
		}
	})
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
