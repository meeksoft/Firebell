package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	// DaemonEnvVar is set when running as daemon child process.
	DaemonEnvVar = "FIREBELL_DAEMON"
)

// Daemon manages the daemon lifecycle.
type Daemon struct {
	dir    string
	lock   *Lock
	logger *Logger
}

// NewDaemon creates a new daemon manager.
func NewDaemon(dir string) *Daemon {
	return &Daemon{
		dir:  dir,
		lock: NewLock(dir),
	}
}

// Start starts the daemon in the background.
// Returns nil if daemon started successfully.
func (d *Daemon) Start(args []string) error {
	// Check if already running
	if running, pid := d.lock.IsRunning(); running {
		return fmt.Errorf("daemon already running (PID %d)", pid)
	}

	// Get the executable path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Ensure log directory exists
	logDir := filepath.Join(d.dir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open log file for daemon output
	logPath := filepath.Join(logDir, fmt.Sprintf("firebell-%s.log", time.Now().Format("2006-01-02")))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Build command with daemon marker
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), DaemonEnvVar+"=1")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil

	// Detach from parent process group
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// Start the daemon
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait briefly and verify it's running
	time.Sleep(100 * time.Millisecond)

	// Check if process is still running
	if cmd.Process != nil {
		// Try to signal the process (signal 0 checks if process exists)
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			return fmt.Errorf("daemon failed to start (check logs at %s)", logPath)
		}
	}

	fmt.Printf("Daemon started (PID %d)\n", cmd.Process.Pid)
	fmt.Printf("Log file: %s\n", logPath)

	return nil
}

// Stop stops the running daemon.
func (d *Daemon) Stop() error {
	running, pid := d.lock.IsRunning()
	if !running {
		return fmt.Errorf("daemon is not running")
	}

	// Send SIGTERM
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process %d: %w", pid, err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	// Wait for process to exit (with timeout)
	for i := 0; i < 50; i++ { // 5 seconds total
		time.Sleep(100 * time.Millisecond)
		if err := process.Signal(syscall.Signal(0)); err != nil {
			fmt.Printf("Daemon stopped (was PID %d)\n", pid)
			return nil
		}
	}

	// Force kill if still running
	if err := process.Signal(syscall.SIGKILL); err == nil {
		fmt.Printf("Daemon killed (was PID %d)\n", pid)
		return nil
	}

	return fmt.Errorf("daemon did not stop (PID %d)", pid)
}

// Restart restarts the daemon.
func (d *Daemon) Restart(args []string) error {
	// Stop if running (ignore error if not running)
	if running, _ := d.lock.IsRunning(); running {
		if err := d.Stop(); err != nil {
			return fmt.Errorf("failed to stop daemon: %w", err)
		}
		// Wait a bit for cleanup
		time.Sleep(200 * time.Millisecond)
	}

	return d.Start(args)
}

// Status returns the daemon status.
func (d *Daemon) Status() (running bool, pid int, uptime time.Duration) {
	running, pid = d.lock.IsRunning()
	if !running {
		return false, 0, 0
	}

	// Try to get process start time for uptime
	uptime = d.getProcessUptime(pid)
	return running, pid, uptime
}

// getProcessUptime returns the uptime of a process.
func (d *Daemon) getProcessUptime(pid int) time.Duration {
	// Read /proc/[pid]/stat to get start time
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return 0
	}

	// Parse stat file - field 22 is starttime (in clock ticks)
	fields := splitStatFields(string(data))
	if len(fields) < 22 {
		return 0
	}

	startTicks, err := strconv.ParseInt(fields[21], 10, 64)
	if err != nil {
		return 0
	}

	// Get system boot time and clock ticks per second
	bootTime := getBootTime()
	clockTicks := int64(100) // Usually 100 Hz on Linux

	if bootTime == 0 {
		return 0
	}

	// Calculate start time in seconds since epoch
	startSec := bootTime + (startTicks / clockTicks)
	startTime := time.Unix(startSec, 0)

	return time.Since(startTime)
}

// splitStatFields splits /proc/[pid]/stat handling comm field with spaces.
func splitStatFields(stat string) []string {
	// comm field (field 2) is in parentheses and may contain spaces
	start := -1
	end := -1
	for i, c := range stat {
		if c == '(' && start == -1 {
			start = i
		}
		if c == ')' {
			end = i
		}
	}

	if start == -1 || end == -1 {
		return nil
	}

	// Build fields: pid, comm, then rest
	var fields []string
	fields = append(fields, stat[:start-1])          // pid
	fields = append(fields, stat[start+1:end])       // comm
	rest := stat[end+2:]                             // skip ") "
	fields = append(fields, splitFields(rest)...)

	return fields
}

// splitFields splits on whitespace.
func splitFields(s string) []string {
	var fields []string
	var field []byte
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' {
			if len(field) > 0 {
				fields = append(fields, string(field))
				field = field[:0]
			}
		} else {
			field = append(field, s[i])
		}
	}
	if len(field) > 0 {
		fields = append(fields, string(field))
	}
	return fields
}

// getBootTime returns the system boot time in seconds since epoch.
func getBootTime() int64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}

	for _, line := range splitFields(string(data)) {
		if len(line) > 6 && line[:6] == "btime " {
			t, _ := strconv.ParseInt(line[6:], 10, 64)
			return t
		}
	}

	// Alternative: parse /proc/stat line by line
	lines := string(data)
	for i := 0; i < len(lines); {
		end := i
		for end < len(lines) && lines[end] != '\n' {
			end++
		}
		line := lines[i:end]
		if len(line) > 6 && line[:6] == "btime " {
			t, _ := strconv.ParseInt(line[6:], 10, 64)
			return t
		}
		i = end + 1
	}

	return 0
}

// IsDaemon returns true if running as daemon child process.
func IsDaemon() bool {
	return os.Getenv(DaemonEnvVar) == "1"
}

// Lock returns the daemon lock.
func (d *Daemon) Lock() *Lock {
	return d.lock
}

// Dir returns the daemon directory.
func (d *Daemon) Dir() string {
	return d.dir
}
