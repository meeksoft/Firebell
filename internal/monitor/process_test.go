package monitor

import (
	"testing"
	"time"
)

func TestProcessMonitor(t *testing.T) {
	t.Run("new process monitor", func(t *testing.T) {
		pm := NewProcessMonitor([]string{"claude", "codex"})
		if pm == nil {
			t.Fatal("NewProcessMonitor returned nil")
		}
		if len(pm.candidates) != 2 {
			t.Errorf("candidates len = %d, want 2", len(pm.candidates))
		}
	})

	t.Run("set PID manually", func(t *testing.T) {
		pm := NewProcessMonitor(nil)
		pm.SetPID(1234)

		if pm.pid != 1234 {
			t.Errorf("pid = %d, want 1234", pm.pid)
		}
		if !pm.cacheValid {
			t.Error("cacheValid should be true after SetPID")
		}
	})

	t.Run("set PID to zero clears cache", func(t *testing.T) {
		pm := NewProcessMonitor(nil)
		pm.SetPID(1234)
		pm.SetPID(0)

		if pm.cacheValid {
			t.Error("cacheValid should be false after SetPID(0)")
		}
	})

	t.Run("idle detection", func(t *testing.T) {
		pm := NewProcessMonitor(nil)
		pm.lastCPU = 0.5 // Low CPU

		// First check should start idle timer
		if pm.CheckIdle(1.0, 100*time.Millisecond) {
			t.Error("should not notify immediately")
		}
		if pm.idleSince.IsZero() {
			t.Error("idleSince should be set")
		}

		// Wait for idle duration
		time.Sleep(150 * time.Millisecond)

		// Second check should trigger notification
		if !pm.CheckIdle(1.0, 100*time.Millisecond) {
			t.Error("should notify after idle duration")
		}

		// Third check should not notify again
		if pm.CheckIdle(1.0, 100*time.Millisecond) {
			t.Error("should not notify twice")
		}
	})

	t.Run("idle reset on activity", func(t *testing.T) {
		pm := NewProcessMonitor(nil)
		pm.lastCPU = 0.5
		pm.CheckIdle(1.0, 100*time.Millisecond)

		// Simulate CPU spike
		pm.lastCPU = 5.0
		pm.CheckIdle(1.0, 100*time.Millisecond)

		if !pm.idleSince.IsZero() {
			t.Error("idleSince should be reset on CPU activity")
		}
		if pm.idleNotified {
			t.Error("idleNotified should be reset on CPU activity")
		}
	})
}

func TestGetProcessCandidates(t *testing.T) {
	agents := []Agent{
		{Name: "claude", ProcessNames: []string{"claude", "claude-code"}},
		{Name: "codex", ProcessNames: []string{"codex"}},
		{Name: "gemini", ProcessNames: []string{"gemini", "claude"}}, // Duplicate 'claude'
	}

	candidates := GetProcessCandidates(agents)

	// Should have unique names: claude, claude-code, codex, gemini
	if len(candidates) != 4 {
		t.Errorf("candidates len = %d, want 4 (unique)", len(candidates))
	}

	// Check for expected names
	seen := make(map[string]bool)
	for _, c := range candidates {
		seen[c] = true
	}

	expected := []string{"claude", "claude-code", "codex", "gemini"}
	for _, e := range expected {
		if !seen[e] {
			t.Errorf("missing candidate: %s", e)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{500, "500B"},
		{1024, "1.0KiB"},
		{1536, "1.5KiB"},
		{1048576, "1.0MiB"},
		{1073741824, "1.0GiB"},
	}

	for _, tt := range tests {
		got := HumanBytes(tt.input)
		if got != tt.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatProcMeta(t *testing.T) {
	t.Run("nil sample", func(t *testing.T) {
		result := FormatProcMeta(nil)
		if result != "" {
			t.Errorf("expected empty string, got %q", result)
		}
	})

	t.Run("with sample", func(t *testing.T) {
		sample := &ProcSample{
			RSSBytes: 1048576, // 1 MiB
			VSZBytes: 2097152, // 2 MiB
			State:    "S",
		}
		result := FormatProcMeta(sample)

		if !containsStr(result, "RSS=1.0MiB") {
			t.Errorf("result %q missing RSS", result)
		}
		if !containsStr(result, "VSZ=2.0MiB") {
			t.Errorf("result %q missing VSZ", result)
		}
		if !containsStr(result, "STAT=S") {
			t.Errorf("result %q missing STAT", result)
		}
	})
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
