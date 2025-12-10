// Package config provides the interactive setup wizard for firebell v2.0.
package config

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AgentInfo holds basic agent information for the setup wizard.
// This avoids importing the monitor package.
type AgentInfo struct {
	Name        string
	DisplayName string
	LogPath     string
}

// SetupAgentProvider is a function type for getting agent information.
// This allows setup to work without importing monitor directly.
type SetupAgentProvider func() []AgentInfo

// SetupWebhookTester is a function type for testing webhooks.
type SetupWebhookTester func(webhook string) error

// SetupOptions configures the setup wizard.
type SetupOptions struct {
	GetAgents     SetupAgentProvider
	TestWebhook   SetupWebhookTester
}

// SetupWizard runs the interactive configuration wizard.
func SetupWizard(opts SetupOptions) error {
	fmt.Println()
	fmt.Printf("Welcome to firebell %s setup!\n", Version)
	fmt.Println()

	cfg := DefaultConfig()
	reader := bufio.NewReader(os.Stdin)

	// Step 1: Notification destination
	if err := setupNotification(reader, cfg, opts.TestWebhook); err != nil {
		return err
	}

	// Step 2: Agent selection
	if err := setupAgents(reader, cfg, opts.GetAgents); err != nil {
		return err
	}

	// Step 3: Process monitoring
	if err := setupProcessMonitoring(reader, cfg); err != nil {
		return err
	}

	// Step 4: Output verbosity
	if err := setupVerbosity(reader, cfg); err != nil {
		return err
	}

	// Save configuration
	configPath := DefaultConfigPath()
	if err := ensureConfigDir(); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := Save(cfg, configPath); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Printf("Configuration saved to %s\n", configPath)
	fmt.Println()
	fmt.Println("Run 'firebell' to start monitoring!")
	fmt.Println()

	return nil
}

// setupNotification configures notification settings.
func setupNotification(reader *bufio.Reader, cfg *Config, testWebhook SetupWebhookTester) error {
	fmt.Println("[1/4] Notification destination")
	fmt.Println("  1. Slack webhook")
	fmt.Println("  2. Stdout (testing)")
	fmt.Println()

	choice := promptChoice(reader, "Choice", 1, 2)

	switch choice {
	case 1:
		cfg.Notify.Type = "slack"
		fmt.Println()
		webhook := promptString(reader, "Enter Slack webhook URL")
		cfg.Notify.Slack.Webhook = webhook

		// Test the webhook
		if testWebhook != nil {
			fmt.Print("Testing webhook... ")
			if err := testWebhook(webhook); err != nil {
				fmt.Println("FAILED")
				fmt.Printf("  Error: %v\n", err)
				fmt.Println("  You can edit the webhook URL later in the config file.")
			} else {
				fmt.Println("Success!")
			}
		}

	case 2:
		cfg.Notify.Type = "stdout"
		fmt.Println("  Notifications will be printed to stdout.")
	}

	fmt.Println()
	return nil
}

// setupAgents configures which agents to monitor.
func setupAgents(reader *bufio.Reader, cfg *Config, getAgents SetupAgentProvider) error {
	fmt.Println("[2/4] Which AI agents to monitor?")
	fmt.Println()

	var agents []AgentInfo
	if getAgents != nil {
		agents = getAgents()
	}

	if len(agents) == 0 {
		fmt.Println("  No agents registered.")
		cfg.Agents.Enabled = nil
		fmt.Println()
		return nil
	}

	// Detect active agents
	var activeNames []string
	fmt.Println("  Detected agents:")
	for _, agent := range agents {
		expanded := expandPath(agent.LogPath)
		info, err := os.Stat(expanded)

		if err != nil {
			fmt.Printf("    ✗ %s (not found)\n", agent.DisplayName)
		} else {
			activeNames = append(activeNames, agent.Name)
			age := ""
			if !info.ModTime().IsZero() {
				since := time.Since(info.ModTime())
				if since < time.Hour {
					age = fmt.Sprintf("%dm ago", int(since.Minutes()))
				} else if since < 24*time.Hour {
					age = fmt.Sprintf("%dh ago", int(since.Hours()))
				} else {
					age = fmt.Sprintf("%dd ago", int(since.Hours()/24))
				}
			}
			fmt.Printf("    ✓ %s (%s - %s)\n", agent.DisplayName, expanded, age)
		}
	}
	fmt.Println()

	if len(activeNames) > 0 {
		fmt.Print("Monitor detected agents only? [Y/n]: ")
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response == "" || response == "y" || response == "yes" {
			// Use auto-detection
			cfg.Agents.Enabled = nil
		} else {
			// Let user select
			cfg.Agents.Enabled = selectAgents(reader, agents)
		}
	} else {
		fmt.Println("No active agents detected. Please select agents to monitor:")
		cfg.Agents.Enabled = selectAgents(reader, agents)
	}

	fmt.Println()
	return nil
}

// selectAgents prompts user to select agents from a list.
func selectAgents(reader *bufio.Reader, agents []AgentInfo) []string {
	fmt.Println()
	for i, agent := range agents {
		fmt.Printf("  %d. %s\n", i+1, agent.DisplayName)
	}
	fmt.Println()

	fmt.Print("Enter agent numbers (comma-separated, e.g., 1,2): ")
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(response)

	var selected []string
	for _, part := range strings.Split(response, ",") {
		part = strings.TrimSpace(part)
		if idx, err := strconv.Atoi(part); err == nil && idx >= 1 && idx <= len(agents) {
			selected = append(selected, agents[idx-1].Name)
		}
	}

	if len(selected) == 0 {
		// Default to all
		for _, a := range agents {
			selected = append(selected, a.Name)
		}
	}
	return selected
}

// setupProcessMonitoring configures process monitoring.
func setupProcessMonitoring(reader *bufio.Reader, cfg *Config) error {
	fmt.Println("[3/4] Process monitoring")
	fmt.Print("Track CPU/memory for completion detection? [Y/n]: ")

	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	cfg.Monitor.ProcessTracking = response == "" || response == "y" || response == "yes"
	cfg.Monitor.CompletionDetection = cfg.Monitor.ProcessTracking

	if cfg.Monitor.ProcessTracking {
		fmt.Println("  Process tracking enabled.")
	} else {
		fmt.Println("  Process tracking disabled.")
	}

	fmt.Println()
	return nil
}

// setupVerbosity configures output verbosity.
func setupVerbosity(reader *bufio.Reader, cfg *Config) error {
	fmt.Println("[4/4] Output verbosity")
	fmt.Println("  1. Minimal (title only)")
	fmt.Println("  2. Normal (title + message) [default]")
	fmt.Println("  3. Verbose (title + message + log snippets)")
	fmt.Println()

	choice := promptChoice(reader, "Choice", 1, 3)

	switch choice {
	case 1:
		cfg.Output.Verbosity = "minimal"
		cfg.Output.IncludeSnippets = false
	case 2, 0:
		cfg.Output.Verbosity = "normal"
		cfg.Output.IncludeSnippets = true
	case 3:
		cfg.Output.Verbosity = "verbose"
		cfg.Output.IncludeSnippets = true
	}

	return nil
}

// promptChoice prompts for a numeric choice within a range.
func promptChoice(reader *bufio.Reader, prompt string, min, max int) int {
	for {
		fmt.Printf("%s [%d-%d]: ", prompt, min, max)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if input == "" {
			return 0 // Default
		}

		choice, err := strconv.Atoi(input)
		if err != nil || choice < min || choice > max {
			fmt.Printf("  Please enter a number between %d and %d\n", min, max)
			continue
		}
		return choice
	}
}

// promptString prompts for a string value.
func promptString(reader *bufio.Reader, prompt string) string {
	fmt.Printf("%s: ", prompt)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if len(path) == 0 {
		return path
	}
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if len(path) == 1 {
			return home
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

// ensureConfigDir creates the config directory if it doesn't exist.
func ensureConfigDir() error {
	dir := filepath.Dir(DefaultConfigPath())
	return os.MkdirAll(dir, 0755)
}

// DefaultTestWebhook provides a default webhook tester.
func DefaultTestWebhook(webhook string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	payload := fmt.Sprintf(`{"text":"firebell %s - Test notification"}`, Version)
	req, err := http.NewRequestWithContext(ctx, "POST", webhook, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}
	return nil
}
