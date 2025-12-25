//go:build linux
// +build linux

package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// detectPID scans /proc to find a matching process.
func (pm *ProcessMonitor) detectPID() int {
	if len(pm.candidates) == 0 {
		return 0
	}

	type found struct {
		pid int
		ts  time.Time
	}
	var latest found

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}

		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}

		cmd := string(cmdline)
		matched := false
		for _, name := range pm.candidates {
			if strings.Contains(cmd, name) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		statPath := filepath.Join("/proc", entry.Name(), "stat")
		info, err := os.Stat(statPath)
		if err != nil {
			continue
		}

		if info.ModTime().After(latest.ts) {
			latest = found{pid: pid, ts: info.ModTime()}
		}
	}

	return latest.pid
}

// ReadProcSample reads process stats from /proc/{pid}/stat.
func ReadProcSample(pid int) (ProcSample, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ProcSample{}, err
	}

	content := string(data)
	end := strings.LastIndex(content, ")")
	if end == -1 {
		return ProcSample{}, fmt.Errorf("bad stat format")
	}

	rest := strings.TrimSpace(content[end+1:])
	fields := strings.Fields(rest)
	if len(fields) < 22 {
		return ProcSample{}, fmt.Errorf("short stat data")
	}

	state := fields[0]
	utime, _ := strconv.ParseFloat(fields[11], 64)
	stime, _ := strconv.ParseFloat(fields[12], 64)
	vsz, _ := strconv.ParseInt(fields[20], 10, 64)
	rssPages, _ := strconv.ParseInt(fields[21], 10, 64)

	const userHz = 100.0
	cpuSeconds := (utime + stime) / userHz
	pageSize := int64(os.Getpagesize())

	return ProcSample{
		CPUSeconds: cpuSeconds,
		Wall:       time.Now(),
		RSSBytes:   rssPages * pageSize,
		VSZBytes:   vsz,
		State:      state,
	}, nil
}

// IsProcessMonitoringSupported returns true on platforms where process monitoring works.
func IsProcessMonitoringSupported() bool {
	return true
}
