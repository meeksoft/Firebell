// Package wrap provides command wrapping functionality for firebell.
// It allows running commands through firebell to monitor their output in real-time.
package wrap

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// PTY wraps a command with a pseudo-terminal for interactive use.
type PTY struct {
	cmd     *exec.Cmd
	pty     *os.File
	oldState *term.State
}

// NewPTY creates a new PTY wrapper for the given command.
func NewPTY(name string, args ...string) *PTY {
	cmd := exec.Command(name, args...)
	return &PTY{cmd: cmd}
}

// Start starts the command with a pseudo-terminal.
// Returns a reader for the command's output.
func (p *PTY) Start() (io.Reader, error) {
	// Start command with pty
	ptmx, err := pty.Start(p.cmd)
	if err != nil {
		return nil, err
	}
	p.pty = ptmx

	// Handle terminal resize
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if err := pty.InheritSize(os.Stdin, ptmx); err != nil {
				// Ignore errors
			}
		}
	}()
	ch <- syscall.SIGWINCH // Initial resize

	// Set stdin to raw mode for proper terminal handling
	if term.IsTerminal(int(os.Stdin.Fd())) {
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			// Non-fatal: continue without raw mode
		} else {
			p.oldState = oldState
		}
	}

	// Copy stdin to pty in background
	go func() {
		io.Copy(ptmx, os.Stdin)
	}()

	return ptmx, nil
}

// Wait waits for the command to finish and returns its exit code.
func (p *PTY) Wait() (int, error) {
	err := p.cmd.Wait()

	// Restore terminal state
	if p.oldState != nil {
		term.Restore(int(os.Stdin.Fd()), p.oldState)
	}

	// Close pty
	if p.pty != nil {
		p.pty.Close()
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

// Close cleans up resources.
func (p *PTY) Close() {
	if p.oldState != nil {
		term.Restore(int(os.Stdin.Fd()), p.oldState)
	}
	if p.pty != nil {
		p.pty.Close()
	}
}
