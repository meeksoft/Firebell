// Package monitor provides core monitoring functionality for AI CLI agents.
package monitor

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Agent represents a supported AI CLI tool with its configuration.
type Agent struct {
	Name         string   // Internal name (lowercase)
	DisplayName  string   // Human-readable name
	LogPath      string   // Default log path (with ~ for home)
	LogPatterns  []string // Glob patterns for log files
	ProcessNames []string // Process names for PID detection
	// Matcher will be added in Phase 2 (detect package)
}

// Registry is the centralized list of all supported AI agents.
// This is the single source of truth for agent metadata.
var Registry = map[string]Agent{
	"claude": {
		Name:         "claude",
		DisplayName:  "Claude Code",
		LogPath:      "~/.claude/projects",
		LogPatterns:  []string{"*.jsonl"},
		ProcessNames: []string{"claude", "claude-code"},
	},
	"codex": {
		Name:         "codex",
		DisplayName:  "Codex",
		LogPath:      "~/.codex/sessions",
		LogPatterns:  []string{"*.jsonl", "*.json"},
		ProcessNames: []string{"codex"},
	},
	"copilot": {
		Name:         "copilot",
		DisplayName:  "GitHub Copilot",
		LogPath:      "~/.copilot/session-state",
		LogPatterns:  []string{"*.jsonl"},
		ProcessNames: []string{"copilot"},
	},
	"gemini": {
		Name:         "gemini",
		DisplayName:  "Google Gemini",
		LogPath:      "~/.gemini/tmp",
		LogPatterns:  []string{"*.json"},
		ProcessNames: []string{"gemini"},
	},
	"opencode": {
		Name:         "opencode",
		DisplayName:  "OpenCode",
		LogPath:      "~/.opencode/logs",
		LogPatterns:  []string{"*.log"},
		ProcessNames: []string{"opencode"},
	},
}

// GetAgent returns the agent definition for the given name.
// Returns nil if not found.
func GetAgent(name string) *Agent {
	if agent, ok := Registry[strings.ToLower(name)]; ok {
		return &agent
	}
	return nil
}

// GetAgents returns agents based on the filter list.
// If filter is empty or nil, returns auto-detected active agents.
// If filter contains specific names, returns only those agents.
func GetAgents(filter []string) []Agent {
	if len(filter) == 0 {
		return DetectActiveAgents()
	}

	var agents []Agent
	for _, name := range filter {
		if agent := GetAgent(name); agent != nil {
			agents = append(agents, *agent)
		}
	}
	return agents
}

// DetectActiveAgents scans the filesystem for agents with recent log activity.
// An agent is considered "active" if its log directory exists and has been
// modified within the last 24 hours.
func DetectActiveAgents() []Agent {
	var active []Agent

	for _, agent := range Registry {
		expanded := ExpandPath(agent.LogPath)

		// Check if path exists
		info, err := os.Stat(expanded)
		if err != nil {
			continue
		}

		// If it's a directory, check for recent modifications
		if info.IsDir() {
			if hasRecentActivity(expanded, 24*time.Hour) {
				active = append(active, agent)
			}
		} else {
			// If it's a file, check its modification time
			if time.Since(info.ModTime()) < 24*time.Hour {
				active = append(active, agent)
			}
		}
	}

	return active
}

// hasRecentActivity checks if a directory has files modified within the duration.
func hasRecentActivity(dir string, within time.Duration) bool {
	var mostRecent time.Time

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Check file extension
		if !hasLogExtension(path) {
			return nil
		}

		if info.ModTime().After(mostRecent) {
			mostRecent = info.ModTime()
		}

		// Early exit if we found recent activity
		if time.Since(mostRecent) < within {
			return filepath.SkipAll
		}

		return nil
	})

	return time.Since(mostRecent) < within
}

// hasLogExtension checks if a file has a log-related extension.
func hasLogExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".log", ".txt", ".json", ".jsonl":
		return true
	default:
		return false
	}
}

// ExpandPath expands ~ to the user's home directory.
func ExpandPath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}

	if path == "~" {
		return home
	}

	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
}

// AllAgentNames returns a list of all supported agent names.
func AllAgentNames() []string {
	names := make([]string, 0, len(Registry))
	for name := range Registry {
		names = append(names, name)
	}
	return names
}
