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

// PlandexMatcher detects Plandex activity from log files.
// Plandex is a Go-based AI coding agent with server-client architecture.
type PlandexMatcher struct {
	agent string
}

// NewPlandexMatcher creates a new Plandex-specific matcher.
func NewPlandexMatcher() *PlandexMatcher {
	return &PlandexMatcher{agent: "plandex"}
}

// Match implements Matcher for PlandexMatcher.
func (m *PlandexMatcher) Match(line string) *Match {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}

	// Try JSON parsing first
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err == nil {
		// Check for status or type fields
		if status, ok := obj["status"].(string); ok {
			switch status {
			case "building", "running", "streaming":
				return &Match{
					Agent:  m.agent,
					Type:   MatchActivity,
					Reason: "status: " + status,
					Line:   line,
					Meta:   obj,
				}
			case "complete", "finished", "done":
				return &Match{
					Agent:  m.agent,
					Type:   MatchComplete,
					Reason: "status: " + status,
					Line:   line,
					Meta:   obj,
				}
			case "pending", "waiting", "blocked":
				return &Match{
					Agent:  m.agent,
					Type:   MatchHolding,
					Reason: "status: " + status,
					Line:   line,
					Meta:   obj,
				}
			}
		}
		// Any other JSON = activity
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "json activity",
			Line:   line,
			Meta:   obj,
		}
	}

	// Text pattern matching
	lineLower := strings.ToLower(line)

	// Completion patterns
	if strings.Contains(lineLower, "plan complete") || strings.Contains(lineLower, "changes applied") ||
		strings.Contains(lineLower, "finished") || strings.Contains(lineLower, "done building") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchComplete,
			Reason: "completion pattern",
			Line:   line,
		}
	}

	// Waiting/blocked patterns
	if strings.Contains(lineLower, "waiting for") || strings.Contains(lineLower, "confirm") ||
		strings.Contains(lineLower, "review changes") || strings.Contains(lineLower, "pending approval") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchHolding,
			Reason: "waiting pattern",
			Line:   line,
		}
	}

	// Activity patterns
	if strings.Contains(lineLower, "building") || strings.Contains(lineLower, "planning") ||
		strings.Contains(lineLower, "streaming") || strings.Contains(lineLower, "processing") ||
		strings.Contains(lineLower, "loading") || strings.Contains(lineLower, "running") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "activity pattern",
			Line:   line,
		}
	}

	return nil
}

// AiderMatcher detects Aider activity from history and log files.
// Aider is a Python-based AI pair programming assistant.
type AiderMatcher struct {
	agent string
}

// NewAiderMatcher creates a new Aider-specific matcher.
func NewAiderMatcher() *AiderMatcher {
	return &AiderMatcher{agent: "aider"}
}

// Match implements Matcher for AiderMatcher.
func (m *AiderMatcher) Match(line string) *Match {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}

	// Aider chat history is markdown format
	// LLM history contains request/response data

	// Check for assistant/model response markers
	if strings.HasPrefix(line, "####") || strings.HasPrefix(line, "---") {
		// Section separator in chat history
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "section marker",
			Line:   line,
		}
	}

	// Check for completion patterns
	lineLower := strings.ToLower(line)
	if strings.Contains(lineLower, "applied edit") || strings.Contains(lineLower, "wrote") ||
		strings.Contains(lineLower, "created") || strings.Contains(lineLower, "updated") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchComplete,
			Reason: "edit applied",
			Line:   line,
		}
	}

	// Check for prompt/waiting patterns
	if strings.Contains(lineLower, "y/n") || strings.Contains(lineLower, "confirm") ||
		strings.Contains(lineLower, "proceed?") || strings.Contains(lineLower, "allow?") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchHolding,
			Reason: "confirmation prompt",
			Line:   line,
		}
	}

	// Check for thinking/working patterns
	if strings.Contains(lineLower, "thinking") || strings.Contains(lineLower, "searching") ||
		strings.Contains(lineLower, "analyzing") || strings.Contains(lineLower, "generating") {
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "working",
			Line:   line,
		}
	}

	// Try JSON parsing for LLM history
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err == nil {
		// Check for role field (OpenAI format)
		if role, ok := obj["role"].(string); ok {
			if role == "assistant" {
				// Check for tool calls
				if _, hasTools := obj["tool_calls"]; hasTools {
					return &Match{
						Agent:  m.agent,
						Type:   MatchHolding,
						Reason: "tool call",
						Line:   line,
						Meta:   obj,
					}
				}
				return &Match{
					Agent:  m.agent,
					Type:   MatchComplete,
					Reason: "assistant response",
					Line:   line,
					Meta:   obj,
				}
			}
		}
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "json activity",
			Line:   line,
			Meta:   obj,
		}
	}

	// Generic content detection - any substantial line is activity
	if len(trimmed) > 20 {
		return &Match{
			Agent:  m.agent,
			Type:   MatchActivity,
			Reason: "content",
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

// FallbackMatcher provides intelligent pattern matching for unknown AI agents.
// It combines JSON parsing with common text patterns to detect activity,
// completion, and tool permission states across various AI CLI tools.
type FallbackMatcher struct {
	agent string
}

// NewFallbackMatcher creates a new fallback matcher for unknown agents.
func NewFallbackMatcher(agentName string) *FallbackMatcher {
	return &FallbackMatcher{agent: agentName}
}

// Match implements Matcher for FallbackMatcher.
func (m *FallbackMatcher) Match(line string) *Match {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}

	// Try JSON parsing first - most AI tools use structured logging
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(line), &obj); err == nil {
		return m.matchJSON(line, obj)
	}

	// Fall back to text pattern matching
	return m.matchText(line, trimmed)
}

// matchJSON handles JSON-formatted log lines
func (m *FallbackMatcher) matchJSON(line string, obj map[string]interface{}) *Match {
	// Check for common type/role fields
	typ, _ := obj["type"].(string)
	role, _ := obj["role"].(string)
	status, _ := obj["status"].(string)
	event, _ := obj["event"].(string)

	// Normalize to lowercase for comparison
	typLower := strings.ToLower(typ)
	roleLower := strings.ToLower(role)
	statusLower := strings.ToLower(status)
	eventLower := strings.ToLower(event)

	// Check for tool/function calls (Holding)
	if _, hasToolCalls := obj["tool_calls"]; hasToolCalls {
		return &Match{Agent: m.agent, Type: MatchHolding, Reason: "tool_calls", Line: line, Meta: obj}
	}
	if _, hasFunctionCall := obj["function_call"]; hasFunctionCall {
		return &Match{Agent: m.agent, Type: MatchHolding, Reason: "function_call", Line: line, Meta: obj}
	}
	if typLower == "tool_use" || typLower == "tool_call" || typLower == "function_call" {
		return &Match{Agent: m.agent, Type: MatchHolding, Reason: "tool type", Line: line, Meta: obj}
	}

	// Check for completion indicators
	if stopReason, ok := obj["stop_reason"].(string); ok {
		switch stopReason {
		case "end_turn", "stop":
			return &Match{Agent: m.agent, Type: MatchComplete, Reason: "stop_reason: " + stopReason, Line: line, Meta: obj}
		case "tool_use":
			return &Match{Agent: m.agent, Type: MatchHolding, Reason: "stop_reason: tool_use", Line: line, Meta: obj}
		}
	}
	if finishReason, ok := obj["finish_reason"].(string); ok {
		switch finishReason {
		case "stop", "end":
			return &Match{Agent: m.agent, Type: MatchComplete, Reason: "finish_reason: " + finishReason, Line: line, Meta: obj}
		case "tool_calls", "function_call":
			return &Match{Agent: m.agent, Type: MatchHolding, Reason: "finish_reason: " + finishReason, Line: line, Meta: obj}
		}
	}

	// Check for OpenAI-style nested choices array
	if choices, ok := obj["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if finishReason, ok := choice["finish_reason"].(string); ok {
				switch finishReason {
				case "stop", "end":
					return &Match{Agent: m.agent, Type: MatchComplete, Reason: "finish_reason: " + finishReason, Line: line, Meta: obj}
				case "tool_calls", "function_call":
					return &Match{Agent: m.agent, Type: MatchHolding, Reason: "finish_reason: " + finishReason, Line: line, Meta: obj}
				}
			}
		}
	}

	// Check status field
	switch statusLower {
	case "complete", "completed", "done", "finished", "success":
		return &Match{Agent: m.agent, Type: MatchComplete, Reason: "status: " + status, Line: line, Meta: obj}
	case "pending", "waiting", "blocked", "paused":
		return &Match{Agent: m.agent, Type: MatchHolding, Reason: "status: " + status, Line: line, Meta: obj}
	case "running", "processing", "streaming", "generating":
		return &Match{Agent: m.agent, Type: MatchActivity, Reason: "status: " + status, Line: line, Meta: obj}
	}

	// Check event field
	switch eventLower {
	case "complete", "turn_complete", "response_complete", "turn_end":
		return &Match{Agent: m.agent, Type: MatchComplete, Reason: "event: " + event, Line: line, Meta: obj}
	case "tool_request", "permission_required", "confirmation_needed":
		return &Match{Agent: m.agent, Type: MatchHolding, Reason: "event: " + event, Line: line, Meta: obj}
	}

	// Check type field for assistant messages
	if typLower == "assistant" || typLower == "gemini" || typLower == "ai" || typLower == "model" {
		return &Match{Agent: m.agent, Type: MatchActivity, Reason: "type: " + typ, Line: line, Meta: obj}
	}

	// Check role field
	if roleLower == "assistant" || roleLower == "ai" || roleLower == "model" {
		return &Match{Agent: m.agent, Type: MatchActivity, Reason: "role: " + role, Line: line, Meta: obj}
	}

	// Check for message/content fields (generic activity)
	if _, hasMessage := obj["message"]; hasMessage {
		return &Match{Agent: m.agent, Type: MatchActivity, Reason: "has message", Line: line, Meta: obj}
	}
	if _, hasContent := obj["content"]; hasContent {
		return &Match{Agent: m.agent, Type: MatchActivity, Reason: "has content", Line: line, Meta: obj}
	}
	if _, hasResponse := obj["response"]; hasResponse {
		return &Match{Agent: m.agent, Type: MatchActivity, Reason: "has response", Line: line, Meta: obj}
	}

	return nil
}

// matchText handles plain text log lines
func (m *FallbackMatcher) matchText(line, trimmed string) *Match {
	lineLower := strings.ToLower(trimmed)

	// Holding patterns - waiting for user input/permission
	holdingPatterns := []string{
		"waiting for", "confirm", "permission", "approve", "allow",
		"y/n", "yes/no", "proceed?", "continue?", "accept?",
		"review changes", "pending approval", "requires confirmation",
	}
	for _, pattern := range holdingPatterns {
		if strings.Contains(lineLower, pattern) {
			return &Match{Agent: m.agent, Type: MatchHolding, Reason: "text: " + pattern, Line: line}
		}
	}

	// Completion patterns
	completePatterns := []string{
		"complete", "completed", "finished", "done", "success",
		"applied edit", "changes applied", "wrote file", "created file",
		"task complete", "turn complete", "response complete",
	}
	for _, pattern := range completePatterns {
		if strings.Contains(lineLower, pattern) {
			return &Match{Agent: m.agent, Type: MatchComplete, Reason: "text: " + pattern, Line: line}
		}
	}

	// Activity patterns
	activityPatterns := []string{
		"thinking", "generating", "processing", "analyzing", "searching",
		"loading", "streaming", "running", "executing", "building",
		"assistant", "response", "message", "output",
	}
	for _, pattern := range activityPatterns {
		if strings.Contains(lineLower, pattern) {
			return &Match{Agent: m.agent, Type: MatchActivity, Reason: "text: " + pattern, Line: line}
		}
	}

	return nil
}

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
	case "plandex":
		// Plandex uses JSON status and text patterns
		return NewPlandexMatcher()
	case "aider":
		// Aider uses markdown history and JSON LLM logs
		return NewAiderMatcher()
	default:
		// Unknown agents use intelligent fallback matching
		return NewFallbackMatcher(agentName)
	}
}
