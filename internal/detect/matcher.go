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

// CopilotMatcher detects GitHub Copilot completion events.
// Note: Copilot only logs HTTP requests, so awaiting detection is not possible.
type CopilotMatcher struct {
	agent string
}

// NewCopilotMatcher creates a new Copilot-specific matcher.
func NewCopilotMatcher() *CopilotMatcher {
	return &CopilotMatcher{agent: "copilot"}
}

// Match implements Matcher for CopilotMatcher.
func (m *CopilotMatcher) Match(line string) *Match {
	// Copilot logs completion success - this IS the completion signal
	// After quiet period, this will trigger "Cooling"
	// If activity stops without this cue, "Awaiting" will be inferred
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
		// Copilot only logs HTTP requests - no awaiting detection possible
		return NewCopilotMatcher()
	default:
		// Other agents use regex matching
		return MustRegexMatcher(agentName, DefaultPattern)
	}
}
