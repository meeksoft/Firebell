package monitor

import (
	"path/filepath"
	"sync"
	"time"

	"firebell/internal/detect"
)

// State holds all runtime monitoring state for firebell.
// It consolidates what was previously 6 separate maps in v1.
type State struct {
	mu          sync.RWMutex
	agents      map[string]*AgentState    // key: agent name
	instances   map[string]*InstanceState // key: filepath (per-instance mode)
	process     *ProcessState
	perInstance bool // Track each instance separately
}

// AgentState tracks per-agent monitoring state.
// This replaces the scattered state maps from v1: tailerMap, missingNotified,
// dedupe, group, lastCue, and quietSent.
type AgentState struct {
	Agent         Agent            // Agent definition from registry
	LastCue       time.Time        // Last activity detected (replaces lastCue map)
	LastCueType   detect.MatchType // Type of last cue (Complete, Activity, etc.)
	QuietNotified bool             // Whether "cooling" was sent (replaces quietSent map)
	WatchedPaths  []string         // Currently watched file paths

	// Internal state
	lastNotify time.Time // For potential future deduplication
}

// InstanceState tracks per-instance (per-file) monitoring state.
// Used when per_instance mode is enabled.
type InstanceState struct {
	AgentName     string           // Parent agent name
	FilePath      string           // Log file path (unique identifier)
	DisplayName   string           // Human-readable name (derived from path)
	LastCue       time.Time        // Last activity detected
	LastCueType   detect.MatchType // Type of last cue
	QuietNotified bool             // Whether notification was sent
}

// ProcessState tracks monitored process resources.
// Used for CPU/memory monitoring and completion detection.
// Note: ProcSample is defined in process.go.
type ProcessState struct {
	PID        int
	LastSample *ProcSample
	IdleSince  time.Time

	// Notification flags
	IdleNotified bool
	MemNotified  bool
	ExitNotified bool
}

// NewState creates a new State instance with initialized maps.
func NewState(perInstance bool) *State {
	return &State{
		agents:      make(map[string]*AgentState),
		instances:   make(map[string]*InstanceState),
		process:     &ProcessState{},
		perInstance: perInstance,
	}
}

// IsPerInstance returns whether per-instance tracking is enabled.
func (s *State) IsPerInstance() bool {
	return s.perInstance
}

// AddAgent adds or updates an agent's state.
func (s *State) AddAgent(agent Agent) *AgentState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.agents[agent.Name]; ok {
		existing.Agent = agent
		return existing
	}

	state := &AgentState{
		Agent:        agent,
		WatchedPaths: []string{},
	}
	s.agents[agent.Name] = state
	return state
}

// GetAgent returns the state for a specific agent.
func (s *State) GetAgent(name string) *AgentState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agents[name]
}

// GetAgentByPath returns the agent state that owns a given file path.
func (s *State) GetAgentByPath(path string) *AgentState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, agent := range s.agents {
		for _, wp := range agent.WatchedPaths {
			if wp == path {
				return agent
			}
		}
	}
	return nil
}

// GetAllAgents returns all agent states.
func (s *State) GetAllAgents() []*AgentState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agents := make([]*AgentState, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	return agents
}

// RecordCue records that activity was detected for an agent.
// Strong cues (MatchComplete, MatchHolding) are not overwritten by MatchActivity.
func (s *State) RecordCue(agentName string, cueType detect.MatchType) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if agent, ok := s.agents[agentName]; ok {
		agent.LastCue = time.Now()
		agent.QuietNotified = false // Reset quiet notification
		agent.lastNotify = time.Now()

		// MatchActivity is a weak signal - don't overwrite strong cues
		// Strong cues: MatchComplete (turn finished), MatchHolding (tool permission)
		if cueType == detect.MatchActivity {
			// Only record Activity if current cue is also Activity or unset
			if agent.LastCueType == detect.MatchActivity || agent.LastCueType == detect.MatchAwaiting {
				agent.LastCueType = cueType
			}
			// Otherwise keep the existing strong cue type
		} else {
			// Strong cue - always record
			agent.LastCueType = cueType
		}
	}
}

// GetLastCueType returns the type of the last cue for an agent.
func (s *State) GetLastCueType(agentName string) detect.MatchType {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if agent, ok := s.agents[agentName]; ok {
		return agent.LastCueType
	}
	return detect.MatchActivity
}

// MarkQuietNotified marks that the "cooling" notification was sent.
func (s *State) MarkQuietNotified(agentName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if agent, ok := s.agents[agentName]; ok {
		agent.QuietNotified = true
	}
}

// ShouldSendQuiet checks if a quiet notification should be sent.
// Returns true if: agent has had a cue, quiet period has elapsed, and not already notified.
func (s *State) ShouldSendQuiet(agentName string, quietDuration time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agent, ok := s.agents[agentName]
	if !ok {
		return false
	}

	// Must have had at least one cue
	if agent.LastCue.IsZero() {
		return false
	}

	// Must not have already sent notification
	if agent.QuietNotified {
		return false
	}

	// Check if quiet period has elapsed
	return time.Since(agent.LastCue) >= quietDuration
}

// UpdateWatchedPaths updates the list of watched paths for an agent.
func (s *State) UpdateWatchedPaths(agentName string, paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if agent, ok := s.agents[agentName]; ok {
		agent.WatchedPaths = paths
	}
}

// Instance-level methods (for per_instance mode)

// GetOrCreateInstance returns the instance state for a filepath, creating it if needed.
func (s *State) GetOrCreateInstance(agentName, filePath string) *InstanceState {
	s.mu.Lock()
	defer s.mu.Unlock()

	if inst, ok := s.instances[filePath]; ok {
		return inst
	}

	inst := &InstanceState{
		AgentName:   agentName,
		FilePath:    filePath,
		DisplayName: deriveInstanceDisplayName(agentName, filePath),
	}
	s.instances[filePath] = inst
	return inst
}

// RecordInstanceCue records activity for a specific instance.
func (s *State) RecordInstanceCue(filePath string, cueType detect.MatchType) {
	s.mu.Lock()
	defer s.mu.Unlock()

	inst, ok := s.instances[filePath]
	if !ok {
		return
	}

	inst.LastCue = time.Now()
	inst.QuietNotified = false

	// Same strong/weak cue logic as agent-level
	if cueType == detect.MatchActivity {
		if inst.LastCueType == detect.MatchActivity || inst.LastCueType == detect.MatchAwaiting {
			inst.LastCueType = cueType
		}
	} else {
		inst.LastCueType = cueType
	}
}

// GetInstanceCueType returns the cue type for a specific instance.
func (s *State) GetInstanceCueType(filePath string) detect.MatchType {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if inst, ok := s.instances[filePath]; ok {
		return inst.LastCueType
	}
	return detect.MatchActivity
}

// MarkInstanceQuietNotified marks that notification was sent for an instance.
func (s *State) MarkInstanceQuietNotified(filePath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if inst, ok := s.instances[filePath]; ok {
		inst.QuietNotified = true
	}
}

// ShouldSendInstanceQuiet checks if a quiet notification should be sent for an instance.
func (s *State) ShouldSendInstanceQuiet(filePath string, quietDuration time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	inst, ok := s.instances[filePath]
	if !ok {
		return false
	}

	if inst.LastCue.IsZero() {
		return false
	}

	if inst.QuietNotified {
		return false
	}

	return time.Since(inst.LastCue) >= quietDuration
}

// GetAllInstances returns all instance states.
func (s *State) GetAllInstances() []*InstanceState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	instances := make([]*InstanceState, 0, len(s.instances))
	for _, inst := range s.instances {
		instances = append(instances, inst)
	}
	return instances
}

// GetInstance returns the instance state for a filepath.
func (s *State) GetInstance(filePath string) *InstanceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.instances[filePath]
}

// deriveInstanceDisplayName creates a human-readable name from agent and filepath.
// For Claude: "Claude Code (project-abc123)" from ~/.claude/projects/abc123/...
// For others: "Agent (filename)" from the log file name
func deriveInstanceDisplayName(agentName, filePath string) string {
	// Get the directory containing the log file
	dir := filepath.Dir(filePath)
	base := filepath.Base(dir)

	// For Claude, the project hash is in the parent directory
	if agentName == "claude" {
		// ~/.claude/projects/<hash>/... -> use hash
		if base != "projects" && base != ".claude" {
			if len(base) > 8 {
				base = base[:8] // Truncate long hashes
			}
			return "Claude Code (" + base + ")"
		}
	}

	// For other agents, use the filename without extension
	fileName := filepath.Base(filePath)
	ext := filepath.Ext(fileName)
	if ext != "" {
		fileName = fileName[:len(fileName)-len(ext)]
	}

	// Get display name from registry
	if agent := GetAgent(agentName); agent != nil {
		return agent.DisplayName + " (" + fileName + ")"
	}

	return agentName + " (" + fileName + ")"
}

// Process state methods

// GetProcess returns the process state.
func (s *State) GetProcess() *ProcessState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.process
}

// SetPID sets the monitored process ID.
func (s *State) SetPID(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.process.PID = pid
}

// UpdateProcSample updates the process sample.
func (s *State) UpdateProcSample(sample *ProcSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.process.LastSample = sample
}

// MarkProcessIdle marks that an idle notification was sent.
func (s *State) MarkProcessIdle(idleSince time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.process.IdleSince = idleSince
	s.process.IdleNotified = true
}

// ResetProcessIdle resets the idle tracking.
func (s *State) ResetProcessIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.process.IdleSince = time.Time{}
	s.process.IdleNotified = false
}

// MarkMemoryNotified marks that a memory threshold notification was sent.
func (s *State) MarkMemoryNotified() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.process.MemNotified = true
}

// ResetMemoryNotified resets the memory notification flag.
func (s *State) ResetMemoryNotified() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.process.MemNotified = false
}

// MarkProcessExited marks that the process has exited.
func (s *State) MarkProcessExited() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.process.ExitNotified = true
}

// IsProcessExitNotified returns whether exit notification was already sent.
func (s *State) IsProcessExitNotified() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.process.ExitNotified
}
