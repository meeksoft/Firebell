// Package main implements the firebell v2.0 CLI entry point.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"firebell/internal/config"
	"firebell/internal/daemon"
	"firebell/internal/monitor"
	"firebell/internal/notify"
	"firebell/internal/wrap"
)

func main() {
	// Parse command-line flags
	flags := config.ParseFlags()

	// Handle special commands
	if flags.Version {
		fmt.Printf("firebell %s\n", config.Version)
		return
	}

	if flags.Setup {
		runSetup(flags)
		return
	}

	if flags.Migrate {
		if err := config.MigrateConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if flags.Check {
		runHealthCheck(flags)
		return
	}

	if flags.Wrap {
		runWrap(flags)
		return
	}

	// Handle daemon commands
	if flags.DaemonStart {
		runDaemonStart(flags)
		return
	}

	if flags.DaemonStop {
		runDaemonStop()
		return
	}

	if flags.DaemonRestart {
		runDaemonRestart(flags)
		return
	}

	if flags.DaemonStatus {
		runDaemonStatus()
		return
	}

	if flags.DaemonLogs {
		runDaemonLogs(flags)
		return
	}

	if flags.Events {
		runEvents(flags)
		return
	}

	if flags.WebhookTest {
		runWebhookTest(flags)
		return
	}

	if flags.Listen {
		runListen(flags)
		return
	}

	// Load configuration
	cfg, err := config.Load(flags.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintln(os.Stderr, "Run 'firebell --setup' to configure")
		os.Exit(1)
	}

	// Override config with flags
	if flags.Stdout {
		cfg.Notify.Type = "stdout"
	}
	if flags.Verbose {
		cfg.Output.Verbosity = "verbose"
	}

	// Determine which agents to monitor
	var agents []monitor.Agent
	if flags.Agent != "" {
		// Single agent specified
		if agent := monitor.GetAgent(flags.Agent); agent != nil {
			agents = append(agents, *agent)
		} else {
			fmt.Fprintf(os.Stderr, "Unknown agent: %s\n", flags.Agent)
			fmt.Fprintln(os.Stderr, "Supported agents:", monitor.AllAgentNames())
			os.Exit(1)
		}
	} else if len(cfg.Agents.Enabled) > 0 {
		// Use agents from config
		agents = monitor.GetAgents(cfg.Agents.Enabled)
	} else {
		// Auto-detect
		agents = monitor.DetectActiveAgents()
		if len(agents) == 0 {
			fmt.Fprintln(os.Stderr, "No active AI agents detected")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Run 'firebell --check' to see status of all supported agents")
			fmt.Fprintln(os.Stderr, "Or specify an agent: firebell --agent claude")
			os.Exit(1)
		}
	}

	// Run monitoring
	if err := runMonitor(cfg, agents); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runSetup runs the interactive configuration wizard.
func runSetup(flags *config.Flags) {
	opts := config.SetupOptions{
		GetAgents: func() []config.AgentInfo {
			var agents []config.AgentInfo
			for _, name := range monitor.AllAgentNames() {
				agent := monitor.GetAgent(name)
				if agent != nil {
					agents = append(agents, config.AgentInfo{
						Name:        agent.Name,
						DisplayName: agent.DisplayName,
						LogPath:     agent.LogPath,
					})
				}
			}
			return agents
		},
		TestWebhook: config.DefaultTestWebhook,
	}

	if err := config.SetupWizard(opts); err != nil {
		fmt.Fprintf(os.Stderr, "Setup failed: %v\n", err)
		os.Exit(1)
	}
}

// runHealthCheck shows the status of all supported agents.
func runHealthCheck(flags *config.Flags) {
	fmt.Printf("firebell %s - Health Check\n", config.Version)
	fmt.Println()

	// Check config
	configPath := config.DefaultConfigPath()
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config:  %s\n", configPath)
	} else {
		fmt.Printf("Config:  Not configured (run 'firebell --setup')\n")
	}
	fmt.Println()

	// Check agents
	fmt.Println("Agents:")
	activeCount := 0
	for _, name := range monitor.AllAgentNames() {
		agent := monitor.GetAgent(name)
		if agent == nil {
			continue
		}

		expanded := monitor.ExpandPath(agent.LogPath)
		info, err := os.Stat(expanded)

		var status, detail string
		if err != nil {
			status = "✗"
			detail = "not found"
		} else {
			activeCount++
			status = "✓"
			// Calculate age
			age := formatAge(info.ModTime())
			if info.IsDir() {
				// Count log files in directory
				count := countLogFiles(expanded)
				detail = fmt.Sprintf("%d files, %s", count, age)
			} else {
				detail = fmt.Sprintf("file, %s", age)
			}
		}

		fmt.Printf("  %-14s %s %s\n", agent.DisplayName, status, detail)
	}
	fmt.Println()

	// Summary
	if activeCount > 0 {
		fmt.Printf("Found %d active agent(s).\n", activeCount)
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  firebell              Auto-detect and monitor active agents")
		fmt.Println("  firebell --agent X    Monitor specific agent")
		fmt.Println("  firebell --setup      Configure notifications")
	} else {
		fmt.Println("No active agents found.")
		fmt.Println()
		fmt.Println("Start an AI assistant (Claude Code, GitHub Codex, etc.) to generate logs.")
	}
}

// formatAge formats a time as a human-readable age string.
func formatAge(t time.Time) string {
	if t.IsZero() {
		return "unknown age"
	}
	since := time.Since(t)
	switch {
	case since < time.Minute:
		return "just now"
	case since < time.Hour:
		return fmt.Sprintf("%dm ago", int(since.Minutes()))
	case since < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(since.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(since.Hours()/24))
	}
}

// countLogFiles counts log files in a directory recursively (max depth 4).
func countLogFiles(dir string) int {
	count := 0
	maxDepth := 4
	baseDepth := strings.Count(dir, string(filepath.Separator))

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Check depth
		depth := strings.Count(path, string(filepath.Separator)) - baseDepth
		if info.IsDir() {
			if depth >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if depth > maxDepth {
			return nil
		}
		if hasLogExtension(info.Name()) {
			count++
		}
		return nil
	})
	return count
}

// hasLogExtension checks if a filename has a log-like extension.
func hasLogExtension(name string) bool {
	exts := []string{".log", ".jsonl", ".json", ".txt"}
	for _, ext := range exts {
		if len(name) > len(ext) && name[len(name)-len(ext):] == ext {
			return true
		}
	}
	return false
}

// runMonitor starts the main monitoring loop.
func runMonitor(cfg *config.Config, agents []monitor.Agent) error {
	dir := config.DefaultConfigDir()
	isDaemon := daemon.IsDaemon()
	var lock *daemon.Lock
	var logger *daemon.Logger

	// If running as daemon, acquire lock and setup logging
	if isDaemon {
		lock = daemon.NewLock(dir)
		if err := lock.TryLock(); err != nil {
			return fmt.Errorf("failed to acquire lock: %w", err)
		}
		defer lock.Unlock()

		// Run log cleanup
		daemon.CleanupOnStart(dir, cfg.Daemon.LogRetentionDays)

		// Setup logger
		var err error
		logger, err = daemon.NewLogger(dir)
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}
		defer logger.Close()

		logger.Info("firebell daemon starting")
		logger.Info("Config: %s", config.DefaultConfigPath())
	}

	// Create socket server if enabled
	var socketServer *daemon.SocketServer
	var socketNotifier *daemon.SocketNotifier
	var extras []notify.Notifier

	if cfg.Daemon.Socket {
		var err error
		socketServer, err = daemon.NewSocketServer(cfg.Daemon.SocketPath)
		if err != nil {
			if isDaemon {
				logger.Warn("Failed to create socket: %v", err)
			}
		} else {
			socketNotifier = daemon.NewSocketNotifier(socketServer)
			extras = append(extras, socketNotifier)
			if isDaemon {
				logger.Info("Socket: %s", socketServer.Path())
			}
		}
	}

	// Create notifier with extras
	notifier, err := notify.NewNotifierWithExtras(cfg, extras)
	if err != nil {
		return fmt.Errorf("failed to create notifier: %w", err)
	}

	// Emit daemon start event if event file is enabled
	var eventFileNotifier *notify.EventFileNotifier
	if multi, ok := notifier.(*notify.MultiNotifier); ok {
		for _, n := range multi.Secondary() {
			if ef, ok := n.(*notify.EventFileNotifier); ok {
				eventFileNotifier = ef
				break
			}
		}
	}
	if eventFileNotifier != nil {
		eventFileNotifier.EmitDaemonStart()
	}

	// Create watcher
	watcher, err := monitor.NewWatcher(cfg, notifier, agents)
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	// Print startup info
	if isDaemon {
		agentNames := ""
		for i, agent := range agents {
			if i > 0 {
				agentNames += ", "
			}
			agentNames += agent.DisplayName
		}
		logger.Info("Notify: %s", notifier.Name())
		logger.Info("Agents: %s", agentNames)
		logger.Info("Monitoring started")
	} else {
		fmt.Printf("firebell %s - Starting monitoring...\n", config.Version)
		fmt.Printf("  Config: %s\n", config.DefaultConfigPath())
		fmt.Printf("  Notify: %s\n", notifier.Name())
		fmt.Printf("  Agents: ")
		for i, agent := range agents {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(agent.DisplayName)
		}
		fmt.Println()
		fmt.Println()
	}

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start socket server
	if socketServer != nil {
		socketServer.Start(ctx)
	}

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		if isDaemon {
			logger.Info("Received shutdown signal")
		} else {
			fmt.Println("\nShutting down...")
		}
		cancel()
	}()

	// Run watcher (event-driven with polling fallback)
	var runErr error
	if cfg.Advanced.ForcePolling {
		runErr = watcher.RunPolling(ctx)
	} else {
		runErr = watcher.Run(ctx)
	}

	// Emit daemon stop event
	if eventFileNotifier != nil {
		eventFileNotifier.EmitDaemonStop()
	}

	// Close socket server
	if socketServer != nil {
		socketServer.Close()
	}

	// Close multi-notifier if applicable
	if multi, ok := notifier.(*notify.MultiNotifier); ok {
		multi.Close()
	}

	if isDaemon {
		logger.Info("Daemon stopped")
	}

	return runErr
}

// runWrap runs a command with firebell monitoring.
func runWrap(flags *config.Flags) {
	if len(flags.WrapArgs) == 0 {
		fmt.Fprintln(os.Stderr, "Error: no command specified")
		fmt.Fprintln(os.Stderr, "Usage: firebell wrap -- <command> [args...]")
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.Load(flags.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintln(os.Stderr, "Run 'firebell --setup' to configure")
		os.Exit(1)
	}

	// Override config with flags
	if flags.Stdout {
		cfg.Notify.Type = "stdout"
	}
	if flags.Verbose {
		cfg.Output.Verbosity = "verbose"
	}

	// Create notifier
	notifier, err := notify.NewNotifier(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating notifier: %v\n", err)
		os.Exit(1)
	}

	// Create runner
	runner := wrap.NewRunner(cfg, notifier, flags.WrapName)

	// Setup context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Run the wrapped command
	exitCode, err := runner.Run(ctx, flags.WrapArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[firebell] Error: %v\n", err)
		os.Exit(1)
	}

	os.Exit(exitCode)
}

// runDaemonStart starts the daemon in the background.
func runDaemonStart(flags *config.Flags) {
	dir := config.DefaultConfigDir()
	d := daemon.NewDaemon(dir)

	// Build args for daemon process
	args := []string{}
	if flags.ConfigPath != "" {
		args = append(args, "--config", flags.ConfigPath)
	}
	if flags.Agent != "" {
		args = append(args, "--agent", flags.Agent)
	}

	if err := d.Start(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runDaemonStop stops the running daemon.
func runDaemonStop() {
	dir := config.DefaultConfigDir()
	d := daemon.NewDaemon(dir)

	if err := d.Stop(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runDaemonRestart restarts the daemon.
func runDaemonRestart(flags *config.Flags) {
	dir := config.DefaultConfigDir()
	d := daemon.NewDaemon(dir)

	// Build args for daemon process
	args := []string{}
	if flags.ConfigPath != "" {
		args = append(args, "--config", flags.ConfigPath)
	}
	if flags.Agent != "" {
		args = append(args, "--agent", flags.Agent)
	}

	if err := d.Restart(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runDaemonStatus shows the daemon status.
func runDaemonStatus() {
	dir := config.DefaultConfigDir()
	d := daemon.NewDaemon(dir)

	running, pid, uptime := d.Status()

	fmt.Println("firebell daemon status")
	fmt.Println()

	if running {
		fmt.Printf("  Status:  running\n")
		fmt.Printf("  PID:     %d\n", pid)
		fmt.Printf("  Uptime:  %s\n", formatDuration(uptime))
	} else {
		fmt.Printf("  Status:  stopped\n")
	}

	// Show log info
	logDir := filepath.Join(dir, "logs")
	logs, err := daemon.GetLogFiles(logDir)
	if err == nil && len(logs) > 0 {
		fmt.Println()
		fmt.Printf("  Logs:    %d file(s) in %s\n", len(logs), logDir)
		totalSize, _ := daemon.TotalLogSize(logDir)
		fmt.Printf("  Size:    %s\n", formatBytes(totalSize))
	}
}

// runDaemonLogs shows or follows the daemon logs.
func runDaemonLogs(flags *config.Flags) {
	dir := config.DefaultConfigDir()
	logDir := filepath.Join(dir, "logs")

	// Find most recent log file
	logs, err := daemon.GetLogFiles(logDir)
	if err != nil || len(logs) == 0 {
		fmt.Fprintln(os.Stderr, "No log files found")
		fmt.Fprintf(os.Stderr, "Log directory: %s\n", logDir)
		os.Exit(1)
	}

	logPath := logs[0].Path

	if flags.DaemonFollow {
		// Follow mode (tail -f)
		fmt.Printf("Following %s (Ctrl+C to stop)\n\n", logPath)
		if err := tailFollow(logPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Print last N lines
		if err := tailFile(logPath, 50); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

// tailFile prints the last n lines of a file.
func tailFile(path string, n int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Read all lines (simple approach)
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Print last n lines
	start := len(lines) - n
	if start < 0 {
		start = 0
	}
	for _, line := range lines[start:] {
		fmt.Println(line)
	}

	return scanner.Err()
}

// tailFollow follows a file like tail -f.
func tailFollow(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Seek to end
	f.Seek(0, io.SeekEnd)

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	reader := bufio.NewReader(f)
	for {
		select {
		case <-sigCh:
			fmt.Println()
			return nil
		default:
			line, err := reader.ReadString('\n')
			if err == io.EOF {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			if err != nil {
				return err
			}
			fmt.Print(line)
		}
	}
}

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}

// formatBytes formats bytes for display.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// runEvents shows or follows the event file.
func runEvents(flags *config.Flags) {
	// Get event file path
	eventPath := filepath.Join(config.DefaultConfigDir(), "events.jsonl")

	// Check if file exists
	info, err := os.Stat(eventPath)
	if os.IsNotExist(err) {
		fmt.Println("Event file not found.")
		fmt.Println()
		fmt.Printf("Location: %s\n", eventPath)
		fmt.Println()
		fmt.Println("The event file is created when firebell runs with event_file enabled (default).")
		fmt.Println("Start firebell to begin generating events:")
		fmt.Println("  firebell start")
		fmt.Println("  firebell --stdout")
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if flags.EventsFollow {
		// Follow mode
		fmt.Printf("Following %s (Ctrl+C to stop)\n\n", eventPath)
		if err := tailFollow(eventPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Show info and recent events
		fmt.Println("firebell events")
		fmt.Println()
		fmt.Printf("  File:   %s\n", eventPath)
		fmt.Printf("  Size:   %s\n", formatBytes(info.Size()))
		fmt.Printf("  Modified: %s\n", formatAge(info.ModTime()))
		fmt.Println()

		// Count events by type
		eventCounts := countEventTypes(eventPath)
		if len(eventCounts) > 0 {
			fmt.Println("Event counts:")
			for eventType, count := range eventCounts {
				fmt.Printf("  %-15s %d\n", eventType, count)
			}
			fmt.Println()
		}

		// Show recent events
		fmt.Println("Recent events (last 10):")
		if err := tailFile(eventPath, 10); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading events: %v\n", err)
		}
		fmt.Println()
		fmt.Println("Use 'firebell events -f' to follow in real-time")
	}
}

// runWebhookTest tests a webhook endpoint.
func runWebhookTest(flags *config.Flags) {
	if flags.WebhookURL == "" {
		fmt.Fprintln(os.Stderr, "Error: no URL specified")
		fmt.Fprintln(os.Stderr, "Usage: firebell webhook test <url>")
		os.Exit(1)
	}

	fmt.Printf("Testing webhook: %s\n", flags.WebhookURL)
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := notify.TestWebhook(ctx, flags.WebhookURL, nil, 10*time.Second)
	if err != nil {
		fmt.Printf("FAILED: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("SUCCESS: Webhook is working!")
	fmt.Println()
	fmt.Println("Add this webhook to your config (~/.firebell/config.yaml):")
	fmt.Println()
	fmt.Println("notify:")
	fmt.Println("  webhooks:")
	fmt.Printf("    - url: \"%s\"\n", flags.WebhookURL)
	fmt.Println("      events: [\"all\"]  # or [\"cooling\", \"activity\", \"process_exit\"]")
}

// countEventTypes counts events by type in the event file.
func countEventTypes(path string) map[string]int {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	counts := make(map[string]int)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Simple extraction of event type without full JSON parsing
		// Look for "event":"<type>"
		if idx := strings.Index(line, `"event":"`); idx != -1 {
			start := idx + 9
			end := strings.Index(line[start:], `"`)
			if end != -1 {
				eventType := line[start : start+end]
				counts[eventType]++
			}
		}
	}
	return counts
}

// runListen connects to the daemon socket and displays events.
func runListen(flags *config.Flags) {
	socketPath := filepath.Join(config.DefaultConfigDir(), "firebell.sock")

	// Check if socket exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		fmt.Println("Socket not found.")
		fmt.Println()
		fmt.Printf("Location: %s\n", socketPath)
		fmt.Println()
		fmt.Println("The socket is created when firebell daemon runs with socket enabled.")
		fmt.Println("Enable in config (~/.firebell/config.yaml):")
		fmt.Println()
		fmt.Println("daemon:")
		fmt.Println("  socket: true")
		fmt.Println()
		fmt.Println("Then start the daemon:")
		fmt.Println("  firebell start")
		return
	}

	// Connect to socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Printf("Connected to %s\n", socketPath)
	fmt.Println("Listening for events (Ctrl+C to stop)...")
	fmt.Println()

	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nDisconnected")
		conn.Close()
		os.Exit(0)
	}()

	// Read and display events
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println("Connection closed by daemon")
				return
			}
			fmt.Fprintf(os.Stderr, "Read error: %v\n", err)
			return
		}

		if flags.ListenJSON {
			// Raw JSON output
			fmt.Print(line)
		} else {
			// Formatted output
			formatSocketEvent(line)
		}
	}
}

// formatSocketEvent formats a JSON event line for display.
func formatSocketEvent(line string) {
	// Parse the event
	var event struct {
		Type      string    `json:"type"`
		Event     string    `json:"event"`
		Timestamp time.Time `json:"timestamp"`
		Agent     string    `json:"agent"`
		Title     string    `json:"title"`
		Message   string    `json:"message"`
	}

	if err := json.Unmarshal([]byte(line), &event); err != nil {
		fmt.Print(line) // Fallback to raw
		return
	}

	// Handle welcome message
	if event.Type == "welcome" {
		return // Skip welcome
	}

	// Format: [timestamp] Agent: Event - Message
	ts := event.Timestamp.Format("15:04:05")
	if event.Agent != "" && event.Event != "" {
		fmt.Printf("[%s] %s: %s", ts, event.Agent, event.Event)
		if event.Message != "" {
			fmt.Printf(" - %s", event.Message)
		}
		fmt.Println()
	} else if event.Title != "" {
		fmt.Printf("[%s] %s", ts, event.Title)
		if event.Message != "" {
			fmt.Printf(" - %s", event.Message)
		}
		fmt.Println()
	}
}
