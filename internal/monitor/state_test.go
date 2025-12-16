package monitor

import (
	"testing"
	"time"

	"firebell/internal/detect"
)

func TestState(t *testing.T) {
	t.Run("add and get agent", func(t *testing.T) {
		s := NewState(false)
		agent := Agent{Name: "claude", DisplayName: "Claude Code"}

		s.AddAgent(agent)

		got := s.GetAgent("claude")
		if got == nil {
			t.Fatal("expected agent, got nil")
		}
		if got.Agent.Name != "claude" {
			t.Errorf("Name = %q, want 'claude'", got.Agent.Name)
		}
	})

	t.Run("get missing agent returns nil", func(t *testing.T) {
		s := NewState(false)

		got := s.GetAgent("nonexistent")
		if got != nil {
			t.Error("expected nil for missing agent")
		}
	})

	t.Run("record cue updates timestamp", func(t *testing.T) {
		s := NewState(false)
		s.AddAgent(Agent{Name: "claude"})

		before := time.Now()
		s.RecordCue("claude", detect.MatchComplete)
		after := time.Now()

		state := s.GetAgent("claude")
		if state.LastCue.Before(before) || state.LastCue.After(after) {
			t.Error("LastCue not updated correctly")
		}
		if state.QuietNotified {
			t.Error("QuietNotified should be cleared on cue")
		}
		if state.LastCueType != detect.MatchComplete {
			t.Errorf("LastCueType = %v, want MatchComplete", state.LastCueType)
		}
	})

	t.Run("quiet notification lifecycle", func(t *testing.T) {
		s := NewState(false)
		s.AddAgent(Agent{Name: "claude"})

		// Record activity
		s.RecordCue("claude", detect.MatchComplete)

		// Should not send quiet immediately
		if s.ShouldSendQuiet("claude", 1*time.Second) {
			t.Error("should not send quiet immediately after cue")
		}

		// Simulate time passing by directly modifying state
		state := s.GetAgent("claude")
		state.LastCue = time.Now().Add(-2 * time.Second)

		// Now should send quiet
		if !s.ShouldSendQuiet("claude", 1*time.Second) {
			t.Error("should send quiet after duration passed")
		}

		// Mark as notified
		s.MarkQuietNotified("claude")

		// Should not send again
		if s.ShouldSendQuiet("claude", 1*time.Second) {
			t.Error("should not send quiet twice")
		}

		// New cue resets
		s.RecordCue("claude", detect.MatchActivity)
		state = s.GetAgent("claude")
		if state.QuietNotified {
			t.Error("QuietNotified should be cleared after new cue")
		}
	})

	t.Run("update watched paths", func(t *testing.T) {
		s := NewState(false)
		s.AddAgent(Agent{Name: "claude"})

		paths := []string{"/path/one", "/path/two"}
		s.UpdateWatchedPaths("claude", paths)

		state := s.GetAgent("claude")
		if len(state.WatchedPaths) != 2 {
			t.Errorf("WatchedPaths len = %d, want 2", len(state.WatchedPaths))
		}
	})

	t.Run("get all agents", func(t *testing.T) {
		s := NewState(false)
		s.AddAgent(Agent{Name: "claude"})
		s.AddAgent(Agent{Name: "codex"})

		all := s.GetAllAgents()
		if len(all) != 2 {
			t.Errorf("GetAllAgents() len = %d, want 2", len(all))
		}
	})
}

func TestPerInstanceState(t *testing.T) {
	t.Run("per-instance mode enabled", func(t *testing.T) {
		s := NewState(true)
		if !s.IsPerInstance() {
			t.Error("IsPerInstance() should return true")
		}
	})

	t.Run("per-instance mode disabled", func(t *testing.T) {
		s := NewState(false)
		if s.IsPerInstance() {
			t.Error("IsPerInstance() should return false")
		}
	})

	t.Run("create and get instance", func(t *testing.T) {
		s := NewState(true)

		inst := s.GetOrCreateInstance("claude", "/path/to/project1/log.jsonl")
		if inst == nil {
			t.Fatal("expected instance, got nil")
		}
		if inst.AgentName != "claude" {
			t.Errorf("AgentName = %q, want 'claude'", inst.AgentName)
		}
		if inst.FilePath != "/path/to/project1/log.jsonl" {
			t.Errorf("FilePath = %q, want '/path/to/project1/log.jsonl'", inst.FilePath)
		}
		if inst.DisplayName == "" {
			t.Error("DisplayName should not be empty")
		}

		// Get same instance again
		inst2 := s.GetOrCreateInstance("claude", "/path/to/project1/log.jsonl")
		if inst != inst2 {
			t.Error("should return same instance for same path")
		}
	})

	t.Run("separate instances for different paths", func(t *testing.T) {
		s := NewState(true)

		inst1 := s.GetOrCreateInstance("claude", "/path/to/project1/log.jsonl")
		inst2 := s.GetOrCreateInstance("claude", "/path/to/project2/log.jsonl")

		if inst1 == inst2 {
			t.Error("should create separate instances for different paths")
		}

		all := s.GetAllInstances()
		if len(all) != 2 {
			t.Errorf("GetAllInstances() len = %d, want 2", len(all))
		}
	})

	t.Run("instance cue lifecycle", func(t *testing.T) {
		s := NewState(true)
		path := "/path/to/project/log.jsonl"

		s.GetOrCreateInstance("claude", path)

		// Record cue
		before := time.Now()
		s.RecordInstanceCue(path, detect.MatchComplete)
		after := time.Now()

		inst := s.GetInstance(path)
		if inst.LastCue.Before(before) || inst.LastCue.After(after) {
			t.Error("LastCue not updated correctly")
		}
		if inst.LastCueType != detect.MatchComplete {
			t.Errorf("LastCueType = %v, want MatchComplete", inst.LastCueType)
		}
		if inst.QuietNotified {
			t.Error("QuietNotified should be false after cue")
		}
	})

	t.Run("instance quiet notification", func(t *testing.T) {
		s := NewState(true)
		path := "/path/to/project/log.jsonl"

		s.GetOrCreateInstance("claude", path)
		s.RecordInstanceCue(path, detect.MatchComplete)

		// Should not send immediately
		if s.ShouldSendInstanceQuiet(path, 1*time.Second) {
			t.Error("should not send quiet immediately")
		}

		// Simulate time passing
		inst := s.GetInstance(path)
		inst.LastCue = time.Now().Add(-2 * time.Second)

		// Now should send
		if !s.ShouldSendInstanceQuiet(path, 1*time.Second) {
			t.Error("should send quiet after duration")
		}

		// Mark notified
		s.MarkInstanceQuietNotified(path)

		// Should not send again
		if s.ShouldSendInstanceQuiet(path, 1*time.Second) {
			t.Error("should not send quiet twice")
		}
	})

	t.Run("instance cue type retrieval", func(t *testing.T) {
		s := NewState(true)
		path := "/path/to/project/log.jsonl"

		s.GetOrCreateInstance("claude", path)

		// Default should be Activity
		if s.GetInstanceCueType(path) != detect.MatchActivity {
			t.Error("default cue type should be MatchActivity")
		}

		// After recording Holding
		s.RecordInstanceCue(path, detect.MatchHolding)
		if s.GetInstanceCueType(path) != detect.MatchHolding {
			t.Error("cue type should be MatchHolding after recording")
		}
	})

	t.Run("strong cues not overwritten by activity", func(t *testing.T) {
		s := NewState(true)
		path := "/path/to/project/log.jsonl"

		s.GetOrCreateInstance("claude", path)

		// Record a strong cue (Complete)
		s.RecordInstanceCue(path, detect.MatchComplete)
		if s.GetInstanceCueType(path) != detect.MatchComplete {
			t.Error("cue type should be MatchComplete")
		}

		// Activity should not overwrite it
		s.RecordInstanceCue(path, detect.MatchActivity)
		if s.GetInstanceCueType(path) != detect.MatchComplete {
			t.Error("MatchComplete should not be overwritten by MatchActivity")
		}

		// Another strong cue should overwrite
		s.RecordInstanceCue(path, detect.MatchHolding)
		if s.GetInstanceCueType(path) != detect.MatchHolding {
			t.Error("MatchHolding should overwrite MatchComplete")
		}
	})
}

func TestDeriveInstanceDisplayName(t *testing.T) {
	tests := []struct {
		agent    string
		path     string
		contains string
	}{
		{"claude", "/home/user/.claude/projects/abc12345/log.jsonl", "Claude Code (abc12345"},
		{"claude", "/home/user/.claude/projects/verylonghashvalue/log.jsonl", "Claude Code (verylong"},
		{"codex", "/home/user/.codex/sessions/session123.jsonl", "Codex (session123)"},
		{"gemini", "/home/user/.gemini/tmp/output.json", "Gemini (output)"},
		{"unknown", "/some/path/file.log", "unknown (file)"},
	}

	for _, tt := range tests {
		t.Run(tt.agent+"_"+tt.path, func(t *testing.T) {
			got := deriveInstanceDisplayName(tt.agent, tt.path)
			if !containsSubstring(got, tt.contains) {
				t.Errorf("deriveInstanceDisplayName(%q, %q) = %q, want to contain %q", tt.agent, tt.path, got, tt.contains)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
