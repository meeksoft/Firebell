package monitor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"firebell/internal/config"
	"firebell/internal/detect"
	"firebell/internal/notify"
)

// Watcher monitors log files for AI activity using fsnotify.
type Watcher struct {
	cfg      *config.Config
	state    *State
	notifier notify.Notifier
	fsw      *fsnotify.Watcher

	// Per-agent resources
	managers map[string]*TailerManager
	matchers map[string]detect.Matcher

	// Process monitoring
	procMon *ProcessMonitor
	pidDone <-chan struct{} // Closed when monitored process exits
}

// NewWatcher creates a new Watcher.
func NewWatcher(cfg *config.Config, notifier notify.Notifier, agents []Agent) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	w := &Watcher{
		cfg:      cfg,
		state:    NewState(),
		notifier: notifier,
		fsw:      fsw,
		managers: make(map[string]*TailerManager),
		matchers: make(map[string]detect.Matcher),
	}

	// Initialize process monitor if enabled
	if cfg.Monitor.ProcessTracking {
		candidates := GetProcessCandidates(agents)
		w.procMon = NewProcessMonitor(candidates)
	}

	// Initialize per-agent resources
	for _, agent := range agents {
		w.state.AddAgent(agent)

		// Create tailer manager
		basePath := ExpandPath(agent.LogPath)
		w.managers[agent.Name] = NewTailerManager(
			basePath,
			cfg.Advanced.MaxRecentFiles,
			cfg.Advanced.WatchDepth,
			false, // Don't read from beginning
		)

		// Create matcher
		w.matchers[agent.Name] = detect.CreateMatcher(agent.Name)

		// Add watch on base path
		if err := w.addWatch(basePath); err != nil {
			// Non-fatal: directory might not exist yet
			fmt.Fprintf(os.Stderr, "Warning: cannot watch %s: %v\n", basePath, err)
		}
	}

	return w, nil
}

// addWatch adds a watch on a path, creating parent directories if needed.
func (w *Watcher) addWatch(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.IsDir() {
		// Watch directory and subdirectories
		return filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil || !info.IsDir() {
				return nil
			}
			// Check depth
			rel, _ := filepath.Rel(path, p)
			depth := 0
			if rel != "." {
				for _, c := range rel {
					if c == os.PathSeparator {
						depth++
					}
				}
			}
			if depth > w.cfg.Advanced.WatchDepth {
				return filepath.SkipDir
			}
			return w.fsw.Add(p)
		})
	}

	// Watch parent directory for file
	return w.fsw.Add(filepath.Dir(path))
}

// Run starts the watcher event loop.
func (w *Watcher) Run(ctx context.Context) error {
	// Initial file discovery
	w.refreshFiles()

	// Setup process monitoring if enabled
	w.setupProcessMonitoring()

	// Create tickers
	refreshTicker := time.NewTicker(5 * time.Second)
	defer refreshTicker.Stop()

	quietTicker := time.NewTicker(1 * time.Second)
	defer quietTicker.Stop()

	procTicker := time.NewTicker(5 * time.Second)
	defer procTicker.Stop()

	fmt.Println("Watching for activity...")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-w.pidDone:
			// Process exited
			w.handleProcessExit(ctx)
			w.pidDone = nil // Prevent repeated handling

		case event, ok := <-w.fsw.Events:
			if !ok {
				return nil
			}
			w.handleFSEvent(ctx, event)

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "fsnotify error: %v\n", err)

		case <-refreshTicker.C:
			w.refreshFiles()

		case <-quietTicker.C:
			w.checkQuietPeriods(ctx)

		case <-procTicker.C:
			w.sampleProcess(ctx)
		}
	}
}

// handleFSEvent processes a filesystem event.
func (w *Watcher) handleFSEvent(ctx context.Context, event fsnotify.Event) {
	// Only care about writes and creates
	if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
		return
	}

	// Find which agent owns this path
	for name, mgr := range w.managers {
		// Check if path is under this manager's base
		rel, err := filepath.Rel(mgr.BasePath, event.Name)
		if err != nil || len(rel) > 0 && rel[0] == '.' {
			continue
		}

		// Refresh and read
		mgr.RefreshFiles()
		newLines := mgr.ReadAllNew()

		for path, lines := range newLines {
			w.processLines(ctx, name, path, lines)
		}
		return
	}
}

// processLines processes new lines from a file.
func (w *Watcher) processLines(ctx context.Context, agentName, path string, lines []string) {
	matcher := w.matchers[agentName]
	if matcher == nil {
		return
	}

	agentState := w.state.GetAgent(agentName)
	if agentState == nil {
		return
	}

	// Determine if we should send activity notifications
	// - Slack: Never send activity notifications (only "cooling")
	// - stdout normal: Only send "cooling" notifications
	// - stdout verbose: Send all activity notifications
	sendActivity := w.cfg.Notify.Type == "stdout" && w.cfg.Output.Verbosity == "verbose"

	for _, line := range lines {
		if line == "" {
			continue
		}

		match := matcher.Match(line)
		if match == nil {
			continue
		}

		// Handle based on match type
		switch match.Type {
		case detect.MatchComplete:
			// Turn complete - record cue for quiet period tracking
			// After quiet period, this will trigger "Cooling"
			w.state.RecordCue(agentName, detect.MatchComplete)

			// Only send activity notification if verbose stdout mode
			if sendActivity {
				n := notify.NewNotificationFromMatch(
					agentName,
					agentState.Agent.DisplayName,
					match.Reason,
					match.Line,
				)
				if w.cfg.Output.IncludeSnippets {
					n.Snippet = TailSnippet(path, w.cfg.Output.SnippetLines, 500)
				}
				if err := w.notifier.Send(ctx, n); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to send notification: %v\n", err)
				}
			}

		case detect.MatchHolding:
			// Holding for tool permission - send notification immediately with tool name
			// Also record cue so quiet period tracking works
			w.state.RecordCue(agentName, detect.MatchHolding)

			toolName := "unknown"
			if match.Meta != nil {
				if t, ok := match.Meta["tool"].(string); ok {
					toolName = t
				}
			}
			w.sendHoldingNotification(ctx, agentState.Agent.DisplayName, toolName)

		case detect.MatchAwaiting:
			// Explicit awaiting (rare - most agents use MatchComplete + quiet period)
			w.state.RecordCue(agentName, detect.MatchAwaiting)
			w.sendAwaitingNotification(ctx, agentState.Agent.DisplayName, "Awaiting", "Ready for your input")

		case detect.MatchActivity:
			// Normal activity (no completion signal) - record cue for quiet period tracking
			// After quiet period without a MatchComplete, this will trigger inferred "Awaiting"
			w.state.RecordCue(agentName, detect.MatchActivity)

			// Only send activity notification if verbose stdout mode
			if !sendActivity {
				continue
			}

			// Create and send notification
			n := notify.NewNotificationFromMatch(
				agentName,
				agentState.Agent.DisplayName,
				match.Reason,
				match.Line,
			)

			// Add snippet if configured
			if w.cfg.Output.IncludeSnippets {
				n.Snippet = TailSnippet(path, w.cfg.Output.SnippetLines, 500)
			}

			if err := w.notifier.Send(ctx, n); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to send notification: %v\n", err)
			}
		}
	}
}

// sendHoldingNotification sends a holding notification immediately when tool permission is needed.
func (w *Watcher) sendHoldingNotification(ctx context.Context, displayName, toolName string) {
	n := &notify.Notification{
		Agent:   displayName,
		Title:   "Holding",
		Message: fmt.Sprintf("Tool: %s", toolName),
		Time:    time.Now(),
	}

	if err := w.notifier.Send(ctx, n); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to send holding notification: %v\n", err)
	}
}

// sendAwaitingNotification sends an awaiting notification immediately.
func (w *Watcher) sendAwaitingNotification(ctx context.Context, displayName, title, message string) {
	n := &notify.Notification{
		Agent:   displayName,
		Title:   title,
		Message: message,
		Time:    time.Now(),
	}

	if err := w.notifier.Send(ctx, n); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to send awaiting notification: %v\n", err)
	}
}

// refreshFiles refreshes the watched files for all agents.
func (w *Watcher) refreshFiles() {
	for name, mgr := range w.managers {
		paths := mgr.RefreshFiles()
		w.state.UpdateWatchedPaths(name, paths)
	}
}

// checkQuietPeriods checks for quiet period notifications.
// Sends "Cooling" if last cue was MatchComplete (turn finished).
// Sends "Awaiting" if last cue was MatchActivity (no completion signal - inferred waiting).
func (w *Watcher) checkQuietPeriods(ctx context.Context) {
	if !w.cfg.Monitor.CompletionDetection {
		return
	}

	quietDuration := w.cfg.QuietDuration()

	// Get CPU percentage if available
	cpuPct := float64(-1)
	if w.procMon != nil {
		cpuPct = w.procMon.LastCPU()
	}

	for _, agentState := range w.state.GetAllAgents() {
		if w.state.ShouldSendQuiet(agentState.Agent.Name, quietDuration) {
			// Determine notification type based on last cue type
			lastCueType := w.state.GetLastCueType(agentState.Agent.Name)

			var n *notify.Notification
			switch lastCueType {
			case detect.MatchComplete:
				// Turn was completed - send "Cooling" notification
				n = notify.NewQuietNotification(agentState.Agent.DisplayName, cpuPct)

			case detect.MatchActivity:
				// Activity without completion signal - infer "Awaiting"
				// This happens when agent stops mid-turn (likely waiting for permission or blocked)
				n = &notify.Notification{
					Agent:   agentState.Agent.DisplayName,
					Title:   "Awaiting",
					Message: "No activity detected (may be waiting for input)",
					Time:    time.Now(),
				}

			case detect.MatchHolding:
				// Already sent immediate notification, but still quiet - resend reminder
				n = &notify.Notification{
					Agent:   agentState.Agent.DisplayName,
					Title:   "Holding",
					Message: "Still waiting for tool approval",
					Time:    time.Now(),
				}

			default:
				// Default to Cooling for any other case
				n = notify.NewQuietNotification(agentState.Agent.DisplayName, cpuPct)
			}

			if err := w.notifier.Send(ctx, n); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to send notification: %v\n", err)
			}

			w.state.MarkQuietNotified(agentState.Agent.Name)
		}
	}
}

// setupProcessMonitoring initializes process tracking.
func (w *Watcher) setupProcessMonitoring() {
	if w.procMon == nil {
		return
	}

	// Try to detect a PID
	pid := w.procMon.GetPID()
	if pid > 0 {
		w.state.SetPID(pid)
		w.pidDone = WatchPID(pid)
		fmt.Printf("  Tracking process: PID %d\n", pid)
	}
}

// handleProcessExit handles when the monitored process exits.
func (w *Watcher) handleProcessExit(ctx context.Context) {
	if w.state.IsProcessExitNotified() {
		return
	}

	pid := w.state.GetProcess().PID
	n := notify.NewProcessExitNotification(pid)
	if err := w.notifier.Send(ctx, n); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to send notification: %v\n", err)
	}
	w.state.MarkProcessExited()
}

// sampleProcess samples the monitored process.
func (w *Watcher) sampleProcess(ctx context.Context) {
	if w.procMon == nil {
		return
	}

	// If we don't have a PID yet, try to detect one
	if w.procMon.GetPID() <= 0 {
		return
	}

	// Check if PID changed (process restarted)
	currentPID := w.procMon.GetPID()
	statePID := w.state.GetProcess().PID
	if currentPID != statePID && currentPID > 0 {
		w.state.SetPID(currentPID)
		w.pidDone = WatchPID(currentPID)
		fmt.Printf("  Now tracking process: PID %d\n", currentPID)
	}

	// Take a sample
	w.procMon.Sample()

	// Update state with latest sample
	if sample := w.procMon.LastSample(); sample != nil {
		w.state.UpdateProcSample(sample)
	}
}

// Close cleans up watcher resources.
func (w *Watcher) Close() error {
	for _, mgr := range w.managers {
		mgr.Close()
	}
	return w.fsw.Close()
}

// RunPolling runs in polling mode (fallback when fsnotify unavailable).
func (w *Watcher) RunPolling(ctx context.Context) error {
	// Setup process monitoring if enabled
	w.setupProcessMonitoring()

	pollInterval := w.cfg.PollInterval()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	quietTicker := time.NewTicker(1 * time.Second)
	defer quietTicker.Stop()

	procTicker := time.NewTicker(5 * time.Second)
	defer procTicker.Stop()

	fmt.Println("Watching for activity (polling mode)...")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-w.pidDone:
			w.handleProcessExit(ctx)
			w.pidDone = nil

		case <-ticker.C:
			w.pollAllAgents(ctx)

		case <-quietTicker.C:
			w.checkQuietPeriods(ctx)

		case <-procTicker.C:
			w.sampleProcess(ctx)
		}
	}
}

// pollAllAgents polls all agents for new lines.
func (w *Watcher) pollAllAgents(ctx context.Context) {
	for name, mgr := range w.managers {
		mgr.RefreshFiles()
		newLines := mgr.ReadAllNew()

		for path, lines := range newLines {
			w.processLines(ctx, name, path, lines)
		}
	}
}
