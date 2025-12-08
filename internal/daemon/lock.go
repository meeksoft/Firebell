package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Lock represents a flock-based singleton lock.
type Lock struct {
	path string
	file *os.File
}

// NewLock creates a new lock instance.
func NewLock(dir string) *Lock {
	return &Lock{
		path: filepath.Join(dir, "firebell.lock"),
	}
}

// TryLock attempts to acquire an exclusive lock.
// Returns nil if lock acquired, error if already locked or failed.
func (l *Lock) TryLock() error {
	// Ensure directory exists
	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create lock directory: %w", err)
	}

	// Open or create lock file
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}

	// Try to acquire exclusive lock (non-blocking)
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			// Read PID from lock file for better error message
			pid := l.readPID(f)
			if pid > 0 {
				return fmt.Errorf("another instance is running (PID %d)", pid)
			}
			return fmt.Errorf("another instance is already running")
		}
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	// Write our PID to the lock file
	if err := f.Truncate(0); err != nil {
		l.unlock(f)
		return fmt.Errorf("failed to truncate lock file: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		l.unlock(f)
		return fmt.Errorf("failed to seek lock file: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		l.unlock(f)
		return fmt.Errorf("failed to write PID: %w", err)
	}
	if err := f.Sync(); err != nil {
		l.unlock(f)
		return fmt.Errorf("failed to sync lock file: %w", err)
	}

	l.file = f
	return nil
}

// Unlock releases the lock.
func (l *Lock) Unlock() error {
	if l.file == nil {
		return nil
	}
	return l.unlock(l.file)
}

func (l *Lock) unlock(f *os.File) error {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
	l.file = nil
	// Remove lock file
	os.Remove(l.path)
	return nil
}

// readPID reads the PID from a lock file.
func (l *Lock) readPID(f *os.File) int {
	f.Seek(0, 0)
	buf := make([]byte, 32)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return 0
	}
	pidStr := strings.TrimSpace(string(buf[:n]))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0
	}
	return pid
}

// IsRunning checks if another daemon is running.
// Returns (running, pid).
func (l *Lock) IsRunning() (bool, int) {
	f, err := os.Open(l.path)
	if err != nil {
		return false, 0
	}
	defer f.Close()

	// Try to acquire lock (non-blocking)
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == syscall.EWOULDBLOCK {
		// Lock held by another process
		pid := l.readPID(f)
		return true, pid
	}

	// We got the lock, release it
	if err == nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}
	return false, 0
}

// GetPID returns the PID of the running daemon, or 0 if not running.
func (l *Lock) GetPID() int {
	running, pid := l.IsRunning()
	if running {
		return pid
	}
	return 0
}

// Path returns the lock file path.
func (l *Lock) Path() string {
	return l.path
}
