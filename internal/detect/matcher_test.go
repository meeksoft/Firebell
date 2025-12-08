package detect

import (
	"testing"
)

func TestRegexMatcher(t *testing.T) {
	t.Run("matches default pattern", func(t *testing.T) {
		m := MustRegexMatcher("test", DefaultPattern)

		tests := []struct {
			line  string
			match bool
		}{
			{"assistant_message: hello", true},
			{"agent_message received", true},
			{"responses/compact output", true},
			{"random log line", false},
			{"", false},
		}

		for _, tt := range tests {
			result := m.Match(tt.line)
			if (result != nil) != tt.match {
				t.Errorf("Match(%q) = %v, want match=%v", tt.line, result != nil, tt.match)
			}
		}
	})

	t.Run("invalid regex panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for invalid regex")
			}
		}()
		MustRegexMatcher("test", "[invalid")
	})
}

func TestCodexMatcher(t *testing.T) {
	m := NewCodexMatcher()

	tests := []struct {
		name  string
		line  string
		match bool
	}{
		{
			name:  "valid assistant response",
			line:  `{"type":"response_item","payload":{"role":"assistant","content":"hello"}}`,
			match: true,
		},
		{
			name:  "user message - no match",
			line:  `{"type":"response_item","payload":{"role":"user","content":"hello"}}`,
			match: false,
		},
		{
			name:  "wrong type",
			line:  `{"type":"request_item","payload":{"role":"assistant"}}`,
			match: false,
		},
		{
			name:  "invalid json",
			line:  `not valid json`,
			match: false,
		},
		{
			name:  "empty line",
			line:  "",
			match: false,
		},
		{
			name:  "whitespace only",
			line:  "   ",
			match: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.line)
			if (result != nil) != tt.match {
				t.Errorf("Match() = %v, want match=%v", result != nil, tt.match)
			}
			if result != nil && result.Agent != "codex" {
				t.Errorf("Agent = %q, want 'codex'", result.Agent)
			}
		})
	}
}

func TestCopilotMatcher(t *testing.T) {
	m := NewCopilotMatcher()

	tests := []struct {
		line  string
		match bool
	}{
		{"chat/completions succeeded", true},
		{"[info] chat/completions succeeded in 200ms", true},
		{"chat/completions failed", false},
		{"random log", false},
	}

	for _, tt := range tests {
		result := m.Match(tt.line)
		if (result != nil) != tt.match {
			t.Errorf("Match(%q) = %v, want match=%v", tt.line, result != nil, tt.match)
		}
	}
}

func TestComboMatcher(t *testing.T) {
	m := NewComboMatcher(
		NewCodexMatcher(),
		MustRegexMatcher("fallback", "fallback_pattern"),
	)

	t.Run("matches codex json", func(t *testing.T) {
		result := m.Match(`{"type":"response_item","payload":{"role":"assistant"}}`)
		if result == nil {
			t.Error("expected match")
		}
		if result.Agent != "codex" {
			t.Errorf("Agent = %q, want 'codex'", result.Agent)
		}
	})

	t.Run("falls back to regex", func(t *testing.T) {
		result := m.Match("log with fallback_pattern here")
		if result == nil {
			t.Error("expected match")
		}
		if result.Agent != "fallback" {
			t.Errorf("Agent = %q, want 'fallback'", result.Agent)
		}
	})

	t.Run("no match returns nil", func(t *testing.T) {
		result := m.Match("completely unrelated log")
		if result != nil {
			t.Error("expected nil for no match")
		}
	})
}

func TestCreateMatcher(t *testing.T) {
	tests := []string{"claude", "codex", "copilot", "gemini", "opencode"}

	for _, agent := range tests {
		t.Run(agent, func(t *testing.T) {
			m := CreateMatcher(agent)
			if m == nil {
				t.Errorf("CreateMatcher(%q) returned nil", agent)
			}
		})
	}
}
