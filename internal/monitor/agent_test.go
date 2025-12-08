package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGetAgent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantNil  bool
	}{
		{"claude lowercase", "claude", "claude", false},
		{"Claude uppercase", "Claude", "claude", false},
		{"CLAUDE all caps", "CLAUDE", "claude", false},
		{"codex", "codex", "codex", false},
		{"copilot", "copilot", "copilot", false},
		{"gemini", "gemini", "gemini", false},
		{"opencode", "opencode", "opencode", false},
		{"unknown", "unknown", "", true},
		{"empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := GetAgent(tt.input)
			if tt.wantNil {
				if agent != nil {
					t.Errorf("Expected nil, got %v", agent)
				}
				return
			}
			if agent == nil {
				t.Error("Expected non-nil agent")
				return
			}
			if agent.Name != tt.wantName {
				t.Errorf("Expected name '%s', got '%s'", tt.wantName, agent.Name)
			}
		})
	}
}

func TestRegistryCompleteness(t *testing.T) {
	// Verify all agents in registry have required fields
	requiredAgents := []string{"claude", "codex", "copilot", "gemini", "opencode"}

	for _, name := range requiredAgents {
		agent, ok := Registry[name]
		if !ok {
			t.Errorf("Agent '%s' missing from registry", name)
			continue
		}

		if agent.Name == "" {
			t.Errorf("Agent '%s' has empty Name", name)
		}
		if agent.DisplayName == "" {
			t.Errorf("Agent '%s' has empty DisplayName", name)
		}
		if agent.LogPath == "" {
			t.Errorf("Agent '%s' has empty LogPath", name)
		}
		if len(agent.LogPatterns) == 0 {
			t.Errorf("Agent '%s' has no LogPatterns", name)
		}
		if len(agent.ProcessNames) == 0 {
			t.Errorf("Agent '%s' has no ProcessNames", name)
		}
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"tilde only", "~", home},
		{"tilde with slash", "~/", home},
		{"tilde with path", "~/.firebell", filepath.Join(home, ".firebell")},
		{"absolute path", "/tmp/test", "/tmp/test"},
		{"relative path", "test", "test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandPath(tt.path)
			if got != tt.want {
				t.Errorf("ExpandPath(%s) = %s, want %s", tt.path, got, tt.want)
			}
		})
	}
}

func TestHasLogExtension(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"test.log", true},
		{"test.txt", true},
		{"test.json", true},
		{"test.jsonl", true},
		{"test.LOG", true},  // Case insensitive
		{"test.TXT", true},
		{"test.go", false},
		{"test.py", false},
		{"test", false},
		{"/path/to/test.log", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := hasLogExtension(tt.path)
			if got != tt.want {
				t.Errorf("hasLogExtension(%s) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestGetAgents(t *testing.T) {
	tests := []struct {
		name   string
		filter []string
		min    int // Minimum expected agents (for auto-detect)
	}{
		{"empty filter auto-detects", []string{}, 0},
		{"single agent", []string{"claude"}, 1},
		{"multiple agents", []string{"claude", "codex"}, 2},
		{"unknown agent filtered", []string{"claude", "unknown"}, 1},
		{"all unknown", []string{"unknown1", "unknown2"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := GetAgents(tt.filter)
			if len(tt.filter) == 0 {
				// Auto-detect case - just verify it returns something valid
				if len(agents) < tt.min {
					// It's ok if no agents detected in test environment
					t.Logf("Auto-detect returned %d agents (expected min %d)", len(agents), tt.min)
				}
			} else {
				if len(agents) != tt.min {
					t.Errorf("GetAgents(%v) returned %d agents, want %d", tt.filter, len(agents), tt.min)
				}
			}
		})
	}
}

func TestAllAgentNames(t *testing.T) {
	names := AllAgentNames()

	if len(names) != len(Registry) {
		t.Errorf("AllAgentNames() returned %d names, registry has %d", len(names), len(Registry))
	}

	// Verify all expected agents are present
	expectedAgents := map[string]bool{
		"claude":   false,
		"codex":    false,
		"copilot":  false,
		"gemini":   false,
		"opencode": false,
	}

	for _, name := range names {
		if _, ok := expectedAgents[name]; ok {
			expectedAgents[name] = true
		}
	}

	for name, found := range expectedAgents {
		if !found {
			t.Errorf("Agent '%s' not found in AllAgentNames()", name)
		}
	}
}

func TestDetectActiveAgents(t *testing.T) {
	// Create a temporary directory with a test log file
	tmpDir := t.TempDir()
	testLogPath := filepath.Join(tmpDir, "test.log")

	// Create a recent log file
	if err := os.WriteFile(testLogPath, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Manually add a test agent to registry for this test
	oldRegistry := Registry
	Registry = map[string]Agent{
		"test": {
			Name:        "test",
			DisplayName: "Test Agent",
			LogPath:     tmpDir,
			LogPatterns: []string{"*.log"},
		},
	}
	defer func() { Registry = oldRegistry }()

	// Test detection
	agents := DetectActiveAgents()

	// Should detect the test agent since we just created a log file
	if len(agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(agents))
	}

	if len(agents) > 0 && agents[0].Name != "test" {
		t.Errorf("Expected 'test' agent, got '%s'", agents[0].Name)
	}
}

func TestHasRecentActivity(t *testing.T) {
	// Create temp directory with files
	tmpDir := t.TempDir()

	// Create a recent file
	recentFile := filepath.Join(tmpDir, "recent.log")
	if err := os.WriteFile(recentFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Test recent activity
	if !hasRecentActivity(tmpDir, 1*time.Hour) {
		t.Error("Expected recent activity to be detected")
	}

	// Test with very short duration
	if hasRecentActivity(tmpDir, 1*time.Nanosecond) {
		t.Error("Expected no recent activity with nanosecond duration")
	}

	// Test non-existent directory
	if hasRecentActivity("/nonexistent", 1*time.Hour) {
		t.Error("Expected no activity for nonexistent directory")
	}
}
