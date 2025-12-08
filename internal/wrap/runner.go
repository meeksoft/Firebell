package wrap

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"firebell/internal/config"
	"firebell/internal/detect"
	"firebell/internal/notify"
)

// Runner executes a command and monitors its output for AI activity.
type Runner struct {
	cfg      *config.Config
	notifier notify.Notifier
	matcher  detect.Matcher
	agentName string
}

// NewRunner creates a new command runner.
func NewRunner(cfg *config.Config, notifier notify.Notifier, agentName string) *Runner {
	// Create a combo matcher that tries all patterns
	matcher := detect.NewComboMatcher(
		detect.NewCodexMatcher(),
		detect.NewCopilotMatcher(),
		detect.MustRegexMatcher("wrapped", detect.DefaultPattern),
	)

	return &Runner{
		cfg:       cfg,
		notifier:  notifier,
		matcher:   matcher,
		agentName: agentName,
	}
}

// Run executes the command and monitors its output.
// Returns the command's exit code.
func (r *Runner) Run(ctx context.Context, args []string) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("no command specified")
	}

	// Create PTY wrapper
	p := NewPTY(args[0], args[1:]...)

	// Start the command
	output, err := p.Start()
	if err != nil {
		return 1, fmt.Errorf("failed to start command: %w", err)
	}
	defer p.Close()

	// Create a pipe to tee output
	pr, pw := io.Pipe()

	// Tee output to both stdout and our monitor
	go func() {
		defer pw.Close()
		mw := io.MultiWriter(os.Stdout, pw)
		io.Copy(mw, output)
	}()

	// Monitor output in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.monitorOutput(ctx, pr)
	}()

	// Wait for command to finish
	exitCode, err := p.Wait()

	// Wait for monitor to finish
	<-done

	return exitCode, err
}

// monitorOutput reads output line by line and checks for matches.
func (r *Runner) monitorOutput(ctx context.Context, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	// Increase buffer size for long lines
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var recentLines []string
	const maxRecentLines = 10

	for scanner.Scan() {
		line := scanner.Text()

		// Keep recent lines for context
		recentLines = append(recentLines, line)
		if len(recentLines) > maxRecentLines {
			recentLines = recentLines[1:]
		}

		// Check for match
		match := r.matcher.Match(line)
		if match != nil {
			r.sendNotification(ctx, match, recentLines)
		}
	}
}

// sendNotification sends a notification for a detected match.
func (r *Runner) sendNotification(ctx context.Context, match *detect.Match, recentLines []string) {
	displayName := r.agentName
	if displayName == "" {
		displayName = "Wrapped Command"
	}

	n := notify.NewNotificationFromMatch(
		"wrapped",
		displayName,
		match.Reason,
		match.Line,
	)

	// Add snippet from recent lines if configured
	if r.cfg.Output.IncludeSnippets && len(recentLines) > 0 {
		snippetLines := r.cfg.Output.SnippetLines
		if snippetLines > len(recentLines) {
			snippetLines = len(recentLines)
		}
		start := len(recentLines) - snippetLines
		if start < 0 {
			start = 0
		}
		n.Snippet = strings.Join(recentLines[start:], "\n")
	}

	if err := r.notifier.Send(ctx, n); err != nil {
		fmt.Fprintf(os.Stderr, "\n[firebell] Failed to send notification: %v\n", err)
	}
}

// RunSimple runs a command without PTY (for non-interactive use).
func (r *Runner) RunSimple(ctx context.Context, args []string) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("no command specified")
	}

	return r.Run(ctx, args)
}
