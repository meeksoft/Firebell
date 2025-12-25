//go:build !linux
// +build !linux

package monitor

import (
	"fmt"
	"time"
)

// detectPID always returns 0 on non-Linux platforms.
// Process monitoring via /proc is not available.
func (pm *ProcessMonitor) detectPID() int {
	return 0
}

// ReadProcSample returns an error on non-Linux platforms.
// Process monitoring via /proc is not available.
func ReadProcSample(pid int) (ProcSample, error) {
	return ProcSample{}, fmt.Errorf("process monitoring not supported on this platform")
}

// IsProcessMonitoringSupported returns false on non-Linux platforms.
func IsProcessMonitoringSupported() bool {
	return false
}
