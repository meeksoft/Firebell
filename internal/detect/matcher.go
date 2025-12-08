// Package detect provides pattern matching for AI CLI activity detection.
package detect

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Match represents a detected activity match.
type Match struct {
	Agent  string                 // Agent name (e.g., "claude", "codex")
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

// CodexMatcher detects Codex assistant responses in JSONL format.
// Codex logs are structured JSONL with type:"response_item" and payload.role:"assistant".
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

	// Check for assistant role in payload
	payload, ok := obj["payload"].(map[string]interface{})
	if !ok {
		return nil
	}

	role, ok := payload["role"].(string)
	if !ok || role != "assistant" {
		return nil
	}

	return &Match{
		Agent:  m.agent,
		Reason: "assistant response",
		Line:   line,
		Meta:   obj,
	}
}

// CopilotMatcher detects GitHub Copilot completion events.
type CopilotMatcher struct {
	agent string
}

// NewCopilotMatcher creates a new Copilot-specific matcher.
func NewCopilotMatcher() *CopilotMatcher {
	return &CopilotMatcher{agent: "copilot"}
}

// Match implements Matcher for CopilotMatcher.
func (m *CopilotMatcher) Match(line string) *Match {
	// Copilot logs completion success
	if strings.Contains(line, "chat/completions succeeded") {
		return &Match{
			Agent:  m.agent,
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
const DefaultPattern = `assistant_message|agent_message|responses/compact`

// CreateMatcher creates the appropriate matcher for an agent.
func CreateMatcher(agentName string) Matcher {
	switch agentName {
	case "codex":
		// Codex uses structured JSONL, but also try regex as fallback
		return NewComboMatcher(
			NewCodexMatcher(),
			MustRegexMatcher(agentName, DefaultPattern),
		)
	case "copilot":
		// Copilot has specific success markers
		return NewComboMatcher(
			NewCopilotMatcher(),
			MustRegexMatcher(agentName, DefaultPattern),
		)
	default:
		// Other agents use regex matching
		return MustRegexMatcher(agentName, DefaultPattern)
	}
}
