// Package main implements the firebell v2.0 CLI entry point.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
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
	fmt.Println("firebell v2.0 - Health Check")
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

// countLogFiles counts log files in a directory.
func countLogFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			name := e.Name()
			if hasLogExtension(name) {
				count++
			}
		}
	}
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

	// Create notifier
	notifier, err := notify.NewNotifier(cfg)
	if err != nil {
		return fmt.Errorf("failed to create notifier: %w", err)
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
		fmt.Println("firebell v2.0 - Starting monitoring...")
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
