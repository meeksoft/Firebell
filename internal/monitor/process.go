// Package monitor provides process monitoring with caching for firebell v2.0.
// This implementation uses github.com/shirou/gopsutil for cross-platform support.
package monitor

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

// ProcSample represents a snapshot of process resource usage.
// This implementation works on Linux, macOS, Windows, and other platforms.
type ProcSample struct {
	CPUSeconds float64   // Cumulative CPU time (user + system) in seconds
	Wall       time.Time // Wall clock time when sample was taken
	RSSBytes   int64     // Resident set size (physical memory) in bytes
	VSZBytes   int64     // Virtual memory size in bytes
	State      string    // Process state (R/S/D/Z/T/etc) - platform-dependent
}

// ProcessMonitor tracks a process and caches the PID to avoid repeated scans.
type ProcessMonitor struct {
	pid            int           // Cached PID (0 = not detected)
	lastSample     *ProcSample   // Most recent sample
	lastCPU        float64       // Last calculated CPU percentage
	idleSince      time.Time     // When CPU first went below threshold
	idleNotified   bool          // Whether idle notification was sent
	candidates     []string      // Process names to search for
	cacheValid     bool          // Whether cached PID is still valid
	lastDetect     time.Time     // Last time we scanned for processes
	detectCooldown time.Duration // Minimum time between process scans
}

// NewProcessMonitor creates a new process monitor for the given candidate process names.
func NewProcessMonitor(candidates []string) *ProcessMonitor {
	return &ProcessMonitor{
		candidates:     candidates,
		detectCooldown: 10 * time.Second,
	}
}

// GetPID returns the monitored process ID, auto-detecting if needed.
// Uses caching to avoid repeated process scans.
func (pm *ProcessMonitor) GetPID() int {
	// If we have a cached PID and it's still alive, return it
	if pm.pid > 0 && pm.cacheValid {
		if pm.IsAlive() {
			return pm.pid
		}
		pm.cacheValid = false
		pm.pid = 0
	}

	// Respect cooldown to avoid hammering process list
	if time.Since(pm.lastDetect) < pm.detectCooldown {
		return pm.pid
	}

	// Auto-detect PID
	pm.pid = pm.detectPID()
	pm.lastDetect = time.Now()
	pm.cacheValid = pm.pid > 0

	return pm.pid
}

// SetPID manually sets the PID to monitor (overrides auto-detection).
func (pm *ProcessMonitor) SetPID(pid int) {
	pm.pid = pid
	pm.cacheValid = pid > 0
}

// IsAlive checks if the monitored process is still running.
// Uses platform-appropriate methods.
func (pm *ProcessMonitor) IsAlive() bool {
	if pm.pid <= 0 {
		return false
	}

	// Try gopsutil first (works on all platforms)
	p, err := process.NewProcess(int32(pm.pid))
	if err == nil {
		running, _ := p.IsRunning()
		if running {
			return true
		}
	}

	// Fallback to syscall (works on Unix-like systems)
	return syscall.Kill(pm.pid, 0) == nil
}

// Sample takes a new process sample and returns CPU percentage.
// Returns -1 if sampling fails or no previous sample exists.
func (pm *ProcessMonitor) Sample() float64 {
	pid := pm.GetPID()
	if pid <= 0 {
		return -1
	}

	sample, err := ReadProcSample(pid)
	if err != nil {
		pm.cacheValid = false // PID may have died
		return -1
	}

	if pm.lastSample == nil {
		pm.lastSample = &sample
		return -1 // Need two samples to calculate
	}

	elapsed := sample.Wall.Sub(pm.lastSample.Wall).Seconds()
	if elapsed <= 0 {
		pm.lastSample = &sample
		return -1
	}

	cpuDelta := sample.CPUSeconds - pm.lastSample.CPUSeconds
	numCPU := float64(runtime.NumCPU())
	pm.lastCPU = (cpuDelta / elapsed) * 100 / numCPU

	pm.lastSample = &sample
	return pm.lastCPU
}

// LastCPU returns the last calculated CPU percentage.
func (pm *ProcessMonitor) LastCPU() float64 {
	return pm.lastCPU
}

// LastSample returns the most recent process sample.
func (pm *ProcessMonitor) LastSample() *ProcSample {
	return pm.lastSample
}

// CheckIdle checks if the process has been idle and returns true if notification should be sent.
// idleThreshold is the CPU percentage below which the process is considered idle.
// idleDuration is how long the process must be idle before notifying.
func (pm *ProcessMonitor) CheckIdle(idleThreshold float64, idleDuration time.Duration) bool {
	if pm.lastCPU < 0 {
		return false
	}

	if pm.lastCPU < idleThreshold {
		if pm.idleSince.IsZero() {
			pm.idleSince = time.Now()
		}
		if !pm.idleNotified && time.Since(pm.idleSince) >= idleDuration {
			pm.idleNotified = true
			return true
		}
	} else {
		pm.idleSince = time.Time{}
		pm.idleNotified = false
	}

	return false
}

// ResetIdleState resets the idle notification state.
func (pm *ProcessMonitor) ResetIdleState() {
	pm.idleSince = time.Time{}
	pm.idleNotified = false
}

// FormatProcMeta formats process metadata for display.
func FormatProcMeta(sample *ProcSample) string {
	if sample == nil {
		return ""
	}
	state := sample.State
	if state == "" {
		state = "?"
	}
	return fmt.Sprintf(" (RSS=%s VSZ=%s STAT=%s)",
		HumanBytes(sample.RSSBytes),
		HumanBytes(sample.VSZBytes),
		state)
}

// HumanBytes formats bytes in human-readable form.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for n >= unit*div && exp < 5 {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	return fmt.Sprintf("%.1f%ciB", value, "KMGTPE"[exp])
}

// WatchPID creates a channel that closes when the specified PID exits.
func WatchPID(pid int) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			p, err := process.NewProcess(int32(pid))
			if err != nil {
				close(done)
				return
			}
			running, _ := p.IsRunning()
			if !running {
				close(done)
				return
			}
		}
	}()
	return done
}

// GetProcessCandidates returns process names to search for based on agents.
func GetProcessCandidates(agents []Agent) []string {
	seen := make(map[string]bool)
	var candidates []string

	for _, agent := range agents {
		for _, name := range agent.ProcessNames {
			if !seen[name] {
				seen[name] = true
				candidates = append(candidates, name)
			}
		}
	}

	return candidates
}

// detectPID scans the process list for matching candidate process names.
// Returns the most recently created matching process.
func (pm *ProcessMonitor) detectPID() int {
	if len(pm.candidates) == 0 {
		return 0
	}

	procs, err := process.Processes()
	if err != nil {
		return 0
	}

	type found struct {
		pid   int32
		create int64
	}
	var latest found

	for _, p := range procs {
		// Get command line to check for matches
		cmdline, err := p.Cmdline()
		if err != nil || cmdline == "" {
			continue
		}

		// Check if this process matches any of our candidates
		matched := false
		for _, name := range pm.candidates {
			if strings.Contains(cmdline, name) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		// Get creation time to find the most recent match
		create, err := p.CreateTime()
		if err != nil {
			continue
		}

		if create > latest.create {
			latest = found{pid: p.Pid, create: create}
		}
	}

	return int(latest.pid)
}

// ReadProcSample reads process stats using gopsutil (cross-platform).
func ReadProcSample(pid int) (ProcSample, error) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return ProcSample{}, fmt.Errorf("failed to open process: %w", err)
	}

	sample := ProcSample{
		Wall: time.Now(),
	}

	// Get CPU times
	times, err := p.Times()
	if err == nil {
		sample.CPUSeconds = times.User + times.System
	}

	// Get memory info
	memInfo, err := p.MemoryInfo()
	if err == nil {
		sample.RSSBytes = int64(memInfo.RSS)
		sample.VSZBytes = int64(memInfo.VMS)
	}

	// Get process status/state (platform-dependent)
	// On Linux/Unix: status field contains state (R/S/D/Z/T)
	// On Windows: this may not be available
	status, err := p.Status()
	if err == nil && len(status) > 0 {
		sample.State = status[0]
	}

	return sample, nil
}

// IsProcessMonitoringSupported returns true on all platforms where gopsutil works.
// This includes Linux, macOS, Windows, FreeBSD, and others.
func IsProcessMonitoringSupported() bool {
	return true
}
