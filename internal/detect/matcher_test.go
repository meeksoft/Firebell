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
		name      string
		line      string
		wantMatch bool
		wantType  MatchType
		wantTool  string
	}{
		{
			name:      "function_call - awaiting permission",
			line:      `{"type":"response_item","payload":{"type":"function_call","name":"shell_command","call_id":"call_123"}}`,
			wantMatch: true,
			wantType:  MatchHolding,
			wantTool:  "shell_command",
		},
		{
			name:      "assistant message with output_text - turn complete",
			line:      `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done!"}]}}`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "assistant message without output_text - activity",
			line:      `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"reasoning","text":"thinking..."}]}}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "user message - no match",
			line:      `{"type":"response_item","payload":{"type":"message","role":"user","content":"hello"}}`,
			wantMatch: false,
		},
		{
			name:      "wrong type",
			line:      `{"type":"request_item","payload":{"role":"assistant"}}`,
			wantMatch: false,
		},
		{
			name:      "event_msg - no match (not response_item)",
			line:      `{"type":"event_msg","payload":{"type":"agent_message","message":"hello"}}`,
			wantMatch: false,
		},
		{
			name:      "invalid json",
			line:      `not valid json`,
			wantMatch: false,
		},
		{
			name:      "empty line",
			line:      "",
			wantMatch: false,
		},
		{
			name:      "whitespace only",
			line:      "   ",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.line)

			if (result != nil) != tt.wantMatch {
				t.Errorf("Match() returned %v, want match=%v", result != nil, tt.wantMatch)
				return
			}

			if result == nil {
				return
			}

			if result.Agent != "codex" {
				t.Errorf("Agent = %q, want 'codex'", result.Agent)
			}

			if result.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", result.Type, tt.wantType)
			}

			if tt.wantTool != "" {
				tool, ok := result.Meta["tool"].(string)
				if !ok || tool != tt.wantTool {
					t.Errorf("Meta[tool] = %q, want %q", tool, tt.wantTool)
				}
			}
		})
	}
}

func TestGeminiMatcher(t *testing.T) {
	m := NewGeminiMatcher()

	tests := []struct {
		name      string
		line      string
		wantMatch bool
		wantType  MatchType
		wantTool  string
	}{
		{
			name:      "gemini type - turn complete",
			line:      `      "type": "gemini",`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "gemini type compact - turn complete",
			line:      `{"type":"gemini","content":"hello"}`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "shell_command tool - awaiting permission",
			line:      `          "name": "run_shell_command",`,
			wantMatch: true,
			wantType:  MatchHolding,
			wantTool:  "run_shell_command",
		},
		{
			name:      "read_file tool - awaiting permission",
			line:      `          "name": "read_file",`,
			wantMatch: true,
			wantType:  MatchHolding,
			wantTool:  "read_file",
		},
		{
			name:      "toolCalls array - activity",
			line:      `      "toolCalls": [`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "user type - no match",
			line:      `      "type": "user",`,
			wantMatch: false,
		},
		{
			name:      "unrelated name field - no match",
			line:      `      "name": "some_other_thing",`,
			wantMatch: false,
		},
		{
			name:      "empty line - no match",
			line:      "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.line)

			if (result != nil) != tt.wantMatch {
				t.Errorf("Match() returned %v, want match=%v", result != nil, tt.wantMatch)
				return
			}

			if result == nil {
				return
			}

			if result.Agent != "gemini" {
				t.Errorf("Agent = %q, want 'gemini'", result.Agent)
			}

			if result.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", result.Type, tt.wantType)
			}

			if tt.wantTool != "" {
				tool, ok := result.Meta["tool"].(string)
				if !ok || tool != tt.wantTool {
					t.Errorf("Meta[tool] = %q, want %q", tool, tt.wantTool)
				}
			}
		})
	}
}

func TestCopilotMatcher(t *testing.T) {
	m := NewCopilotMatcher()

	tests := []struct {
		name      string
		line      string
		wantMatch bool
		wantType  MatchType
		wantTool  string
	}{
		{
			name:      "turn end - complete",
			line:      `{"type":"assistant.turn_end","data":{"turnId":"0"},"id":"abc123"}`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "assistant message with tool requests - holding",
			line:      `{"type":"assistant.message","data":{"toolRequests":[{"name":"bash","arguments":{}}]}}`,
			wantMatch: true,
			wantType:  MatchHolding,
			wantTool:  "bash",
		},
		{
			name:      "assistant message without tool requests - activity",
			line:      `{"type":"assistant.message","data":{"content":"Hello"}}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "tool execution start - activity",
			line:      `{"type":"tool.execution_start","data":{"toolName":"view"}}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "user message - activity",
			line:      `{"type":"user.message","data":{"content":"hello"}}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "legacy completion success",
			line:      "chat/completions succeeded",
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "legacy completion with details",
			line:      "[info] chat/completions succeeded in 200ms",
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "session info - no match",
			line:      `{"type":"session.info","data":{}}`,
			wantMatch: false,
		},
		{
			name:      "random log - no match",
			line:      "random log",
			wantMatch: false,
		},
		{
			name:      "empty line - no match",
			line:      "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.line)

			if (result != nil) != tt.wantMatch {
				t.Errorf("Match() returned %v, want match=%v", result != nil, tt.wantMatch)
				return
			}

			if result == nil {
				return
			}

			if result.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", result.Type, tt.wantType)
			}

			if tt.wantTool != "" {
				tool, ok := result.Meta["tool"].(string)
				if !ok || tool != tt.wantTool {
					t.Errorf("Meta[tool] = %q, want %q", tool, tt.wantTool)
				}
			}
		})
	}
}

func TestComboMatcher(t *testing.T) {
	m := NewComboMatcher(
		NewCodexMatcher(),
		MustRegexMatcher("fallback", "fallback_pattern"),
	)

	t.Run("matches codex json", func(t *testing.T) {
		// Use valid Codex format with message type and assistant role
		result := m.Match(`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}`)
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

func TestClaudeMatcher(t *testing.T) {
	m := NewClaudeMatcher()

	tests := []struct {
		name      string
		line      string
		wantMatch bool
		wantType  MatchType
		wantTool  string
	}{
		{
			name:      "end_turn - turn complete",
			line:      `{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"Done!"}]}}`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "tool_use - awaiting permission",
			line:      `{"type":"assistant","message":{"stop_reason":"tool_use","content":[{"type":"tool_use","name":"Bash","id":"toolu_123"}]}}`,
			wantMatch: true,
			wantType:  MatchHolding,
			wantTool:  "Bash",
		},
		{
			name:      "tool_use with Edit tool",
			line:      `{"type":"assistant","message":{"stop_reason":"tool_use","content":[{"type":"tool_use","name":"Edit","id":"toolu_456"}]}}`,
			wantMatch: true,
			wantType:  MatchHolding,
			wantTool:  "Edit",
		},
		{
			name:      "no stop_reason - activity",
			line:      `{"type":"assistant","message":{"content":[{"type":"text","text":"Working..."}]}}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "stop_reason null - activity",
			line:      `{"type":"assistant","message":{"stop_reason":null,"content":[{"type":"text","text":"Streaming..."}]}}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "user type - no match",
			line:      `{"type":"user","message":{"content":"hello"}}`,
			wantMatch: false,
		},
		{
			name:      "system type - no match",
			line:      `{"type":"system","content":"compacted"}`,
			wantMatch: false,
		},
		{
			name:      "invalid json - no match",
			line:      `not valid json`,
			wantMatch: false,
		},
		{
			name:      "empty line - no match",
			line:      "",
			wantMatch: false,
		},
		{
			name:      "assistant without message object - activity",
			line:      `{"type":"assistant","uuid":"abc123"}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.line)

			if (result != nil) != tt.wantMatch {
				t.Errorf("Match() returned %v, want match=%v", result != nil, tt.wantMatch)
				return
			}

			if result == nil {
				return
			}

			if result.Agent != "claude" {
				t.Errorf("Agent = %q, want 'claude'", result.Agent)
			}

			if result.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", result.Type, tt.wantType)
			}

			if tt.wantTool != "" {
				tool, ok := result.Meta["tool"].(string)
				if !ok || tool != tt.wantTool {
					t.Errorf("Meta[tool] = %q, want %q", tool, tt.wantTool)
				}
			}
		})
	}
}

func TestQwenMatcher(t *testing.T) {
	m := NewQwenMatcher()

	tests := []struct {
		name      string
		line      string
		wantMatch bool
		wantType  MatchType
		wantTool  string
	}{
		{
			name:      "response complete - stop",
			line:      `{"choices":[{"finish_reason":"stop","message":{"content":"Done!"}}]}`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "tool call - holding",
			line:      `{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"function":{"name":"shell_exec"}}]}}]}`,
			wantMatch: true,
			wantType:  MatchHolding,
			wantTool:  "shell_exec",
		},
		{
			name:      "streaming chunk - activity",
			line:      `{"choices":[{"delta":{"content":"Hello"}}]}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "request with messages - activity",
			line:      `{"messages":[{"role":"user","content":"hi"}],"model":"qwen3-coder"}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "invalid json - no match",
			line:      `not valid json`,
			wantMatch: false,
		},
		{
			name:      "empty line - no match",
			line:      "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.line)

			if (result != nil) != tt.wantMatch {
				t.Errorf("Match() returned %v, want match=%v", result != nil, tt.wantMatch)
				return
			}

			if result == nil {
				return
			}

			if result.Agent != "qwen" {
				t.Errorf("Agent = %q, want 'qwen'", result.Agent)
			}

			if result.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", result.Type, tt.wantType)
			}

			if tt.wantTool != "" {
				tool, ok := result.Meta["tool"].(string)
				if !ok || tool != tt.wantTool {
					t.Errorf("Meta[tool] = %q, want %q", tool, tt.wantTool)
				}
			}
		})
	}
}

func TestOpenCodeMatcher(t *testing.T) {
	m := NewOpenCodeMatcher()

	tests := []struct {
		name      string
		line      string
		wantMatch bool
		wantType  MatchType
	}{
		{
			name:      "tool execution - activity",
			line:      `2025-01-09T10:00:00 tool.execute name=bash`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "tool confirmation - holding",
			line:      `2025-01-09T10:00:00 tool.confirm name=bash awaiting confirmation`,
			wantMatch: true,
			wantType:  MatchHolding,
		},
		{
			name:      "turn complete - complete",
			line:      `2025-01-09T10:00:00 turn.complete duration=5s`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "assistant message - activity",
			line:      `2025-01-09T10:00:00 assistant response received`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "unrelated log - no match",
			line:      `2025-01-09T10:00:00 system startup`,
			wantMatch: false,
		},
		{
			name:      "empty line - no match",
			line:      "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.line)

			if (result != nil) != tt.wantMatch {
				t.Errorf("Match() returned %v, want match=%v", result != nil, tt.wantMatch)
				return
			}

			if result == nil {
				return
			}

			if result.Agent != "opencode" {
				t.Errorf("Agent = %q, want 'opencode'", result.Agent)
			}

			if result.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", result.Type, tt.wantType)
			}
		})
	}
}

func TestCrushMatcher(t *testing.T) {
	m := NewCrushMatcher()

	tests := []struct {
		name      string
		line      string
		wantMatch bool
		wantType  MatchType
	}{
		{
			name:      "json tool confirm - holding",
			line:      `{"level":"info","msg":"tool confirm required","tool":"bash"}`,
			wantMatch: true,
			wantType:  MatchHolding,
		},
		{
			name:      "json complete - complete",
			line:      `{"level":"info","msg":"turn complete","duration":"5s"}`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "json activity - activity",
			line:      `{"level":"info","msg":"processing request"}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "text tool permission - holding",
			line:      `time=2025-01-09 tool permission requested for bash`,
			wantMatch: true,
			wantType:  MatchHolding,
		},
		{
			name:      "text complete - complete",
			line:      `time=2025-01-09 task complete`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "text assistant - activity",
			line:      `time=2025-01-09 assistant response`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "unrelated log - no match",
			line:      `time=2025-01-09 system startup`,
			wantMatch: false,
		},
		{
			name:      "empty line - no match",
			line:      "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.line)

			if (result != nil) != tt.wantMatch {
				t.Errorf("Match() returned %v, want match=%v", result != nil, tt.wantMatch)
				return
			}

			if result == nil {
				return
			}

			if result.Agent != "crush" {
				t.Errorf("Agent = %q, want 'crush'", result.Agent)
			}

			if result.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", result.Type, tt.wantType)
			}
		})
	}
}

func TestAmazonQMatcher(t *testing.T) {
	m := NewAmazonQMatcher()

	tests := []struct {
		name      string
		line      string
		wantMatch bool
		wantType  MatchType
		wantTool  string
	}{
		{
			name:      "json tool use - holding",
			line:      `{"type":"tool_use","name":"bash","input":{}}`,
			wantMatch: true,
			wantType:  MatchHolding,
			wantTool:  "bash",
		},
		{
			name:      "json response complete - complete",
			line:      `{"type":"response_complete","content":"Done!"}`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "json activity - activity",
			line:      `{"event":"processing","data":{}}`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "text tool permission - holding",
			line:      `[INFO] tool permission required for bash`,
			wantMatch: true,
			wantType:  MatchHolding,
		},
		{
			name:      "text complete - complete",
			line:      `[INFO] response complete`,
			wantMatch: true,
			wantType:  MatchComplete,
		},
		{
			name:      "text chat - activity",
			line:      `[INFO] chat message received`,
			wantMatch: true,
			wantType:  MatchActivity,
		},
		{
			name:      "unrelated log - no match",
			line:      `[DEBUG] network connection established`,
			wantMatch: false,
		},
		{
			name:      "empty line - no match",
			line:      "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.line)

			if (result != nil) != tt.wantMatch {
				t.Errorf("Match() returned %v, want match=%v", result != nil, tt.wantMatch)
				return
			}

			if result == nil {
				return
			}

			if result.Agent != "amazonq" {
				t.Errorf("Agent = %q, want 'amazonq'", result.Agent)
			}

			if result.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", result.Type, tt.wantType)
			}

			if tt.wantTool != "" {
				tool, ok := result.Meta["tool"].(string)
				if !ok || tool != tt.wantTool {
					t.Errorf("Meta[tool] = %q, want %q", tool, tt.wantTool)
				}
			}
		})
	}
}

func TestCreateMatcher(t *testing.T) {
	tests := []string{"claude", "codex", "copilot", "gemini", "opencode", "crush", "qwen", "amazonq"}

	for _, agent := range tests {
		t.Run(agent, func(t *testing.T) {
			m := CreateMatcher(agent)
			if m == nil {
				t.Errorf("CreateMatcher(%q) returned nil", agent)
			}
		})
	}
}
