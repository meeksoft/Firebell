// Package monitor provides process monitoring with caching for firebell v2.0.
// Platform-specific implementations are in process_linux.go and process_stub.go.
package monitor

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
)

// ProcSample represents a snapshot of process resource usage.
// On Linux, this is read from /proc/{pid}/stat. On other platforms,
// fields will be zero/empty as process monitoring is not supported.
type ProcSample struct {
	CPUSeconds float64   // Cumulative CPU time (user + system) in seconds
	Wall       time.Time // Wall clock time when sample was taken
	RSSBytes   int64     // Resident set size (physical memory) in bytes
	VSZBytes   int64     // Virtual memory size in bytes
	State      string    // Process state (R/S/D/Z/T/etc)
}

// ProcessMonitor tracks a process and caches the PID to avoid repeated /proc scans.
type ProcessMonitor struct {
	pid           int           // Cached PID (0 = not detected)
	lastSample    *ProcSample   // Most recent sample
	lastCPU       float64       // Last calculated CPU percentage
	idleSince     time.Time     // When CPU first went below threshold
	idleNotified  bool          // Whether idle notification was sent
	candidates    []string      // Process names to search for
	cacheValid    bool          // Whether cached PID is still valid
	lastDetect    time.Time     // Last time we scanned /proc
	detectCooldown time.Duration // Minimum time between /proc scans
}

// NewProcessMonitor creates a new process monitor for the given candidate process names.
func NewProcessMonitor(candidates []string) *ProcessMonitor {
	return &ProcessMonitor{
		candidates:     candidates,
		detectCooldown: 10 * time.Second,
	}
}

// GetPID returns the monitored process ID, auto-detecting if needed.
// Uses caching to avoid repeated /proc scans.
func (pm *ProcessMonitor) GetPID() int {
	// If we have a cached PID and it's still alive, return it
	if pm.pid > 0 && pm.cacheValid {
		if pm.IsAlive() {
			return pm.pid
		}
		pm.cacheValid = false
		pm.pid = 0
	}

	// Respect cooldown to avoid hammering /proc
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
func (pm *ProcessMonitor) IsAlive() bool {
	if pm.pid <= 0 {
		return false
	}
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
			if syscall.Kill(pid, 0) != nil {
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
