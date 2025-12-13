// Package detect provides pattern matching for AI CLI activity detection.
package detect

import (
	"encoding/json"
	"regexp"
	"strings"
)

// MatchType indicates the category of match for notification routing.
type MatchType int

const (
	MatchActivity MatchType = iota // Normal activity detection (no completion signal)
	MatchComplete                  // Turn complete, response finished (triggers Cooling after quiet)
	MatchAwaiting                  // Explicit waiting for user input (immediate notification)
	MatchHolding                   // Waiting for tool approval (immediate notification)
)

// Match represents a detected activity match.
type Match struct {
	Agent  string                 // Agent name (e.g., "claude", "codex")
	Type   MatchType              // Category of match for notification routing
	Reason string                 // Why this matched (e.g., "assistant response", "regex match")
	Line   string                 // The matched line
	Meta   map[string]interface{} // Additional metadata (e.g., parsed JSON fields)
}

// Matcher is the interface for detecting AI activity in log lines.
type Matcher interface {
	// Match checks if a line matches the pattern.
	// Returns nil if no match.
	Match(line string) *Match
}

// RegexMatcher matches lines using a regular expression.
type RegexMatcher struct {
	pattern *regexp.Regexp
	agent   string
}

// NewRegexMatcher creates a new regex-based matcher.
func NewRegexMatcher(agent, pattern string) (*RegexMatcher, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &RegexMatcher{
		pattern: re,
		agent:   agent,
	}, nil
}

// MustRegexMatcher creates a regex matcher, panicking on error.
// Use for known-good patterns at initialization.
func MustRegexMatcher(agent, pattern string) *RegexMatcher {
	m, err := NewRegexMatcher(agent, pattern)
	if err != nil {
		panic(err)
	}
	return m
}

// Match implements Matcher for RegexMatcher.
func (m *RegexMatcher) Match(line string) *Match {
	if m.pattern.MatchString(line) {
		return &Match{
			Agent:  m.agent,
			Reason: "regex match",
			Line:   line,
		}
	}
	return nil
}

// CodexMatcher detects Codex activity and awaiting states in JSONL format.
// Codex logs are structured JSONL with type:"response_item" containing payloads.
// Detects:
// - function_call payload = awaiting permission
// - assistant message with output_text = awaiting input (turn complete)
// - other assistant activity = normal activity
type CodexMatcher struct {
	agent string
}

// NewCodexMatcher creates a new Codex-specific matcher.
func NewCodexMatcher() *CodexMatcher {
	return &CodexMatcher{agent: "codex"}
}

// Match implements Matcher for CodexMatcher.
func (m *CodexMatcher) Match(line string) *Match {
	// Skip empty lines
	if len(strings.TrimSpace(line)) == 0 {
		return nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return nil
	}

	// Check for response_item type
	typ, ok := obj["type"].(string)
	if !ok || typ != "response_item" {
		return nil
	}

	payload, ok := obj["payload"].(map[string]interface{})
	if !ok {
		return nil
	}

	payloadType, _ := payload["type"].(string)

	// Check for function_call = awaiting permission
	if payloadType == "function_call" {
		meta := obj
		// Extract function name
		if name, ok := payload["name"].(string); ok {
			if meta == nil {
				meta = make(map[string]interface{})
			}
			meta["tool"] = name
		}
		if callID, ok := payload["call_id"].(string); ok {
			meta["tool_id"] = callID
		}
		return &Match{
			Agent:  m.agent,
			Type:   MatchHolding,
			Reason: "function call",
			Line:   line,
			Meta:   meta,
		}
	}

	// Check for assistant message with output_text = turn complete
	if payloadType == "message" {
		role, _ := payload["role"].(string)
		if role == "assistant" {
			// Check if content contains output_text (final response)
			if content, ok := payload["content"].([]interface{}); ok {
				for _, item := range content {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if itemMap["type"] == "output_text" {
							// This is a complete assistant response - turn finished
							// After quiet period, this will trigger "Cooling"
							return &Match{
								Agent:  m.agent,
								Type:   MatchComplete,
								Reason: "assistant response complete",
								Line:   line,
								Meta:   obj,
							}
						}
					}
				}
			}
			// Assistant message without output_text = still streaming/activity
			return &Match{
				Agent:  m.agent,
				Type:   MatchActivity,
				Reason: "assistant response",
				Line:   line,
				Meta:   obj,
			}
		}
	}

	return nil
}

// ClaudeMatcher detects Claude Code activity and awaiting states in JSONL format.
// Parses structured JSONL with type:"assistant" and stop_reason values.
type ClaudeMatcher struct {
	agent string
}

// NewClaudeMatcher creates a new Claude Code-specific matcher.
func NewClaudeMatcher() *ClaudeMatcher {
	return &ClaudeMatcher{agent: "claude"}
}

// Match implements Matcher for ClaudeMatcher.
func (m *ClaudeMatcher) Match(line string) *Match {
	// Skip empty lines
	if len(strings.TrimSpace(line)) == 0 {
		return nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return nil
	}

	// Must be an assistant type entry
	typ, ok := obj["type"].(string)
	if !ok || typ != "assistant" {
		return nil
	}

	// Get the message object
	message, ok := obj["message"].(map[string]interface{})
	if !ok {
		// Fallback: still activity if it's an assistant entry
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "assistant response",
			Line:   line,
			Meta:   obj,
		}
	}

	// Check stop_reason to determine match type
	stopReason, _ := message["stop_reason"].(string)

	switch stopReason {
	case "end_turn":
		// Claude finished speaking, turn complete
		// After quiet period, this will trigger "Cooling" (or inferred "Awaiting" for user input)
		return &Match{
			Agent:  m.agent,
			Type:   MatchComplete,
			Reason: "end turn",
			Line:   line,
			Meta:   obj,
		}

	case "tool_use":
		// Claude wants to run a tool, waiting for approval
		meta := obj
		// Extract tool name from content
		if content, ok := message["content"].([]interface{}); ok {
			for _, item := range content {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if itemMap["type"] == "tool_use" {
						if toolName, ok := itemMap["name"].(string); ok {
							if meta == nil {
								meta = make(map[string]interface{})
							}
							meta["tool"] = toolName
						}
						if toolID, ok := itemMap["id"].(string); ok {
							meta["tool_id"] = toolID
						}
						break
					}
				}
			}
		}
		return &Match{
			Agent:  m.agent,
			Type:   MatchHolding,
			Reason: "tool use",
			Line:   line,
			Meta:   meta,
		}

	default:
		// Normal activity (streaming or other states)
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "assistant response",
			Line:   line,
			Meta:   obj,
		}
	}
}

// GeminiMatcher detects Gemini CLI activity and awaiting states.
// Gemini uses single JSON files (not JSONL) with a messages array.
// When the file is rewritten, lines containing message data are detected.
// Detects:
// - "type": "gemini" with content = awaiting input (turn complete)
// - toolCalls without completed status = awaiting permission
type GeminiMatcher struct {
	agent string
}

// NewGeminiMatcher creates a new Gemini-specific matcher.
func NewGeminiMatcher() *GeminiMatcher {
	return &GeminiMatcher{agent: "gemini"}
}

// Match implements Matcher for GeminiMatcher.
func (m *GeminiMatcher) Match(line string) *Match {
	// Skip empty lines
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}

	// Gemini files are pretty-printed JSON, so we look for specific patterns
	// Check for gemini message type (indicates assistant response complete)
	if strings.Contains(line, `"type": "gemini"`) || strings.Contains(line, `"type":"gemini"`) {
		return &Match{
			Agent:  m.agent,
			Type:   MatchComplete,
			Reason: "gemini response",
			Line:   line,
		}
	}

	// Check for toolCalls - look for tool call initiation
	// toolCalls appear as objects with name, args, and status
	if strings.Contains(line, `"toolCalls"`) {
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "tool calls",
			Line:   line,
		}
	}

	// Check for individual tool call with name (awaiting permission potentially)
	// Format: "name": "run_shell_command" or "name": "write_todos"
	if strings.Contains(line, `"name":`) && (strings.Contains(line, `shell_command`) ||
		strings.Contains(line, `read_file`) || strings.Contains(line, `write_file`) ||
		strings.Contains(line, `edit_file`) || strings.Contains(line, `list_dir`)) {

		// Try to extract tool name
		toolName := extractToolName(line)
		meta := make(map[string]interface{})
		if toolName != "" {
			meta["tool"] = toolName
		}

		return &Match{
			Agent:  m.agent,
			Type:   MatchHolding,
			Reason: "tool call",
			Line:   line,
			Meta:   meta,
		}
	}

	return nil
}

// extractToolName attempts to extract a tool name from a JSON line
func extractToolName(line string) string {
	// Simple extraction - look for "name": "value"
	idx := strings.Index(line, `"name":`)
	if idx == -1 {
		return ""
	}
	rest := line[idx+7:] // Skip past `"name":`
	rest = strings.TrimSpace(rest)
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:] // Skip opening quote
	endIdx := strings.Index(rest, `"`)
	if endIdx == -1 {
		return ""
	}
	return rest[:endIdx]
}

// CopilotMatcher detects GitHub Copilot activity from session-state JSONL files.
// Parses structured JSONL with type:"assistant.turn_end" for completion
// and type:"tool.execution_start" for activity.
type CopilotMatcher struct {
	agent string
}

// NewCopilotMatcher creates a new Copilot-specific matcher.
func NewCopilotMatcher() *CopilotMatcher {
	return &CopilotMatcher{agent: "copilot"}
}

// Match implements Matcher for CopilotMatcher.
func (m *CopilotMatcher) Match(line string) *Match {
	// Skip empty lines
	if len(strings.TrimSpace(line)) == 0 {
		return nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		// Fallback: check for old log format
		if strings.Contains(line, "chat/completions succeeded") {
			return &Match{
				Agent:  m.agent,
				Type:   MatchComplete,
				Reason: "completion success",
				Line:   line,
			}
		}
		return nil
	}

	typ, ok := obj["type"].(string)
	if !ok {
		return nil
	}

	switch typ {
	case "assistant.turn_end":
		// Turn completed - agent finished responding
		return &Match{
			Agent:  m.agent,
			Type:   MatchComplete,
			Reason: "turn end",
			Line:   line,
			Meta:   obj,
		}

	case "assistant.message":
		// Check for tool requests in the message
		if data, ok := obj["data"].(map[string]interface{}); ok {
			if toolRequests, ok := data["toolRequests"].([]interface{}); ok && len(toolRequests) > 0 {
				// Has tool requests - this is a potential holding point
				meta := obj
				// Extract first tool name
				if len(toolRequests) > 0 {
					if req, ok := toolRequests[0].(map[string]interface{}); ok {
						if name, ok := req["name"].(string); ok {
							if meta == nil {
								meta = make(map[string]interface{})
							}
							meta["tool"] = name
						}
					}
				}
				return &Match{
					Agent:  m.agent,
					Type:   MatchHolding,
					Reason: "tool request",
					Line:   line,
					Meta:   meta,
				}
			}
		}
		// Regular assistant message without tool requests
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "assistant message",
			Line:   line,
			Meta:   obj,
		}

	case "tool.execution_start":
		// Tool is executing - activity
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "tool execution",
			Line:   line,
			Meta:   obj,
		}

	case "user.message":
		// User input - activity
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "user message",
			Line:   line,
			Meta:   obj,
		}
	}

	return nil
}

// QwenMatcher detects Qwen Code activity from OpenAI API logs.
// Qwen Code is a fork of Gemini CLI that logs OpenAI-compatible API calls.
// Logs are JSONL format with request/response data.
type QwenMatcher struct {
	agent string
}

// NewQwenMatcher creates a new Qwen Code-specific matcher.
func NewQwenMatcher() *QwenMatcher {
	return &QwenMatcher{agent: "qwen"}
}

// Match implements Matcher for QwenMatcher.
func (m *QwenMatcher) Match(line string) *Match {
	// Skip empty lines
	if len(strings.TrimSpace(line)) == 0 {
		return nil
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		return nil
	}

	// Check for response object with choices
	if choices, ok := obj["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			// Check finish_reason for completion detection
			if finishReason, ok := choice["finish_reason"].(string); ok {
				switch finishReason {
				case "stop":
					return &Match{
						Agent:  m.agent,
						Type:   MatchComplete,
						Reason: "response complete",
						Line:   line,
						Meta:   obj,
					}
				case "tool_calls", "function_call":
					// Extract tool name if available
					meta := obj
					if message, ok := choice["message"].(map[string]interface{}); ok {
						if toolCalls, ok := message["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
							if tc, ok := toolCalls[0].(map[string]interface{}); ok {
								if fn, ok := tc["function"].(map[string]interface{}); ok {
									if name, ok := fn["name"].(string); ok {
										if meta == nil {
											meta = make(map[string]interface{})
										}
										meta["tool"] = name
									}
								}
							}
						}
					}
					return &Match{
						Agent:  m.agent,
						Type:   MatchHolding,
						Reason: "tool call",
						Line:   line,
						Meta:   meta,
					}
				}
			}
			// No finish_reason = still streaming
			return &Match{
				Agent:  m.agent,
				Type:   MatchActivity,
				Reason: "response chunk",
				Line:   line,
				Meta:   obj,
			}
		}
	}

	// Check for request logging (messages array = new request)
	if _, ok := obj["messages"]; ok {
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "request",
			Line:   line,
			Meta:   obj,
		}
	}

	return nil
}

// OpenCodeMatcher detects SST OpenCode activity from log files.
// OpenCode logs are timestamped text files with structured messages.
type OpenCodeMatcher struct {
	agent string
}

// NewOpenCodeMatcher creates a new OpenCode-specific matcher.
func NewOpenCodeMatcher() *OpenCodeMatcher {
	return &OpenCodeMatcher{agent: "opencode"}
}

// Match implements Matcher for OpenCodeMatcher.
func (m *OpenCodeMatcher) Match(line string) *Match {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}

	// OpenCode logs various events - look for key patterns
	// Tool execution patterns
	if strings.Contains(line, "tool.execute") || strings.Contains(line, "executing tool") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "tool execution",
			Line:   line,
		}
	}

	// Tool permission/confirmation patterns
	if strings.Contains(line, "tool.confirm") || strings.Contains(line, "awaiting confirmation") ||
		strings.Contains(line, "permission") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchHolding,
			Reason: "tool confirmation",
			Line:   line,
		}
	}

	// Turn complete patterns
	if strings.Contains(line, "turn.complete") || strings.Contains(line, "response.complete") ||
		strings.Contains(line, "assistant.done") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchComplete,
			Reason: "turn complete",
			Line:   line,
		}
	}

	// General assistant activity
	if strings.Contains(line, "assistant") || strings.Contains(line, "response") ||
		strings.Contains(line, "message") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "assistant activity",
			Line:   line,
		}
	}

	return nil
}

// CrushMatcher detects Charmbracelet Crush activity from log files.
// Crush uses slog for structured logging.
type CrushMatcher struct {
	agent string
}

// NewCrushMatcher creates a new Crush-specific matcher.
func NewCrushMatcher() *CrushMatcher {
	return &CrushMatcher{agent: "crush"}
}

// Match implements Matcher for CrushMatcher.
func (m *CrushMatcher) Match(line string) *Match {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}

	// Try JSON parsing first (slog can output JSON)
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err == nil {
		// Check for message or msg field
		if msg, ok := obj["msg"].(string); ok {
			if strings.Contains(msg, "tool") && strings.Contains(msg, "confirm") {
				return &Match{
					Agent:  m.agent,
					Type:   MatchHolding,
					Reason: "tool confirmation",
					Line:   line,
					Meta:   obj,
				}
			}
			if strings.Contains(msg, "complete") || strings.Contains(msg, "done") {
				return &Match{
					Agent:  m.agent,
					Type:   MatchComplete,
					Reason: "turn complete",
					Line:   line,
					Meta:   obj,
				}
			}
			// Any other message = activity
			return &Match{
				Agent:  m.agent,
				Type:   MatchActivity,
				Reason: "activity",
				Line:   line,
				Meta:   obj,
			}
		}
	}

	// Fallback to text pattern matching
	if strings.Contains(line, "tool") && (strings.Contains(line, "confirm") || strings.Contains(line, "permission")) {
		return &Match{
			Agent:  m.agent,
			Type:   MatchHolding,
			Reason: "tool confirmation",
			Line:   line,
		}
	}

	if strings.Contains(line, "complete") || strings.Contains(line, "finished") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchComplete,
			Reason: "turn complete",
			Line:   line,
		}
	}

	if strings.Contains(line, "assistant") || strings.Contains(line, "response") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "assistant activity",
			Line:   line,
		}
	}

	return nil
}

// AmazonQMatcher detects Amazon Q CLI activity from log files.
// Amazon Q logs to chat.log and qchat.log files.
type AmazonQMatcher struct {
	agent string
}

// NewAmazonQMatcher creates a new Amazon Q-specific matcher.
func NewAmazonQMatcher() *AmazonQMatcher {
	return &AmazonQMatcher{agent: "amazonq"}
}

// Match implements Matcher for AmazonQMatcher.
func (m *AmazonQMatcher) Match(line string) *Match {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}

	// Try JSON parsing first
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err == nil {
		// Check for event type
		if eventType, ok := obj["type"].(string); ok {
			switch eventType {
			case "tool_use", "tool_call":
				meta := obj
				if name, ok := obj["name"].(string); ok {
					if meta == nil {
						meta = make(map[string]interface{})
					}
					meta["tool"] = name
				}
				return &Match{
					Agent:  m.agent,
					Type:   MatchHolding,
					Reason: "tool use",
					Line:   line,
					Meta:   meta,
				}
			case "response_complete", "turn_complete":
				return &Match{
					Agent:  m.agent,
					Type:   MatchComplete,
					Reason: "response complete",
					Line:   line,
					Meta:   obj,
				}
			}
		}
		// Any other JSON = activity
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "activity",
			Line:   line,
			Meta:   obj,
		}
	}

	// Fallback to text pattern matching
	if strings.Contains(line, "tool") && (strings.Contains(line, "permission") || strings.Contains(line, "confirm")) {
		return &Match{
			Agent:  m.agent,
			Type:   MatchHolding,
			Reason: "tool permission",
			Line:   line,
		}
	}

	if strings.Contains(line, "complete") || strings.Contains(line, "finished") || strings.Contains(line, "done") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchComplete,
			Reason: "complete",
			Line:   line,
		}
	}

	if strings.Contains(line, "response") || strings.Contains(line, "message") || strings.Contains(line, "chat") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "activity",
			Line:   line,
		}
	}

	return nil
}

// ComboMatcher combines multiple matchers, returning the first match.
type ComboMatcher struct {
	matchers []Matcher
}

// NewComboMatcher creates a matcher that tries multiple patterns.
func NewComboMatcher(matchers ...Matcher) *ComboMatcher {
	return &ComboMatcher{matchers: matchers}
}

// Match implements Matcher for ComboMatcher.
func (m *ComboMatcher) Match(line string) *Match {
	for _, matcher := range m.matchers {
		if match := matcher.Match(line); match != nil {
			return match
		}
	}
	return nil
}

// DefaultPattern is the default regex pattern for generic matching.
// Matches Claude ("type":"assistant"), Gemini ("type": "gemini"), and other common patterns.
// Allows optional whitespace after colon for pretty-printed JSON.
const DefaultPattern = `"type":\s*"assistant"|"type":\s*"gemini"|assistant_message|agent_message|responses/compact`

// CreateMatcher creates the appropriate matcher for an agent.
func CreateMatcher(agentName string) Matcher {
	switch agentName {
	case "claude":
		// Claude Code uses structured JSONL with stop_reason for awaiting detection
		return NewClaudeMatcher()
	case "codex":
		// Codex uses structured JSONL with function_call for awaiting detection
		return NewCodexMatcher()
	case "gemini":
		// Gemini uses pretty-printed JSON with type: "gemini" for awaiting detection
		return NewGeminiMatcher()
	case "copilot":
		// Copilot uses session-state JSONL with assistant.turn_end and toolRequests
		return NewCopilotMatcher()
	case "qwen":
		// Qwen Code logs OpenAI-compatible API calls in JSONL format
		return NewQwenMatcher()
	case "opencode":
		// SST OpenCode uses timestamped log files
		return NewOpenCodeMatcher()
	case "crush":
		// Charmbracelet Crush uses slog for structured logging
		return NewCrushMatcher()
	case "amazonq":
		// Amazon Q CLI logs to chat.log and qchat.log
		return NewAmazonQMatcher()
	default:
		// Other agents use regex matching
		return MustRegexMatcher(agentName, DefaultPattern)
	}
}
