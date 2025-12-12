package monitor

import (
	"testing"
	"time"

	"firebell/internal/detect"
)

func TestState(t *testing.T) {
	t.Run("add and get agent", func(t *testing.T) {
		s := NewState()
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
		s := NewState()

		got := s.GetAgent("nonexistent")
		if got != nil {
			t.Error("expected nil for missing agent")
		}
	})

	t.Run("record cue updates timestamp", func(t *testing.T) {
		s := NewState()
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
		s := NewState()
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
		s := NewState()
		s.AddAgent(Agent{Name: "claude"})

		paths := []string{"/path/one", "/path/two"}
		s.UpdateWatchedPaths("claude", paths)

		state := s.GetAgent("claude")
		if len(state.WatchedPaths) != 2 {
			t.Errorf("WatchedPaths len = %d, want 2", len(state.WatchedPaths))
		}
	})

	t.Run("get all agents", func(t *testing.T) {
		s := NewState()
		s.AddAgent(Agent{Name: "claude"})
		s.AddAgent(Agent{Name: "codex"})

		all := s.GetAllAgents()
		if len(all) != 2 {
			t.Errorf("GetAllAgents() len = %d, want 2", len(all))
		}
	})
}
