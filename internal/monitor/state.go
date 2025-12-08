package monitor

import (
	"sync"
	"time"
)

// State holds all runtime monitoring state for ai-chime.
// It consolidates what was previously 6 separate maps in v1.
type State struct {
	mu      sync.RWMutex
	agents  map[string]*AgentState
	process *ProcessState
}

// AgentState tracks per-agent monitoring state.
// This replaces the scattered state maps from v1: tailerMap, missingNotified,
// dedupe, group, lastCue, and quietSent.
type AgentState struct {
	Agent        Agent     // Agent definition from registry
	LastCue      time.Time // Last activity detected (replaces lastCue map)
	QuietNotified bool     // Whether "likely finished" was sent (replaces quietSent map)
	WatchedPaths []string  // Currently watched file paths

	// Internal state
	lastNotify time.Time // For potential future deduplication
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
func NewState() *State {
	return &State{
		agents:  make(map[string]*AgentState),
		process: &ProcessState{},
	}
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
func (s *State) RecordCue(agentName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if agent, ok := s.agents[agentName]; ok {
		agent.LastCue = time.Now()
		agent.QuietNotified = false // Reset quiet notification
		agent.lastNotify = time.Now()
	}
}

// MarkQuietNotified marks that the "likely finished" notification was sent.
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
