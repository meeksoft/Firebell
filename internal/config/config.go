// Package config provides configuration management for firebell v2.0.
// It supports YAML config files with backward compatibility for v1 JSON configs.
package config

import (
	"time"
)

// Config is the root configuration structure for firebell v2.0.
type Config struct {
	Version  string         `yaml:"version" json:"version"`
	Notify   NotifyConfig   `yaml:"notify" json:"notify"`
	Agents   AgentsConfig   `yaml:"agents" json:"agents"`
	Monitor  MonitorConfig  `yaml:"monitor" json:"monitor"`
	Output   OutputConfig   `yaml:"output" json:"output"`
	Daemon   DaemonConfig   `yaml:"daemon" json:"daemon"`
	Advanced AdvancedConfig `yaml:"advanced" json:"advanced"`
}

// DaemonConfig defines daemon mode settings.
type DaemonConfig struct {
	LogRetentionDays int `yaml:"log_retention_days" json:"log_retention_days"` // Days to keep logs (0 = forever)

	// Event file settings for external integrations
	EventFile        bool   `yaml:"event_file" json:"event_file"`                 // Enable event file output
	EventFilePath    string `yaml:"event_file_path" json:"event_file_path"`       // Path to event file (default: ~/.firebell/events.jsonl)
	EventFileMaxSize int64  `yaml:"event_file_max_size" json:"event_file_max_size"` // Max size in bytes before rotation (default: 10MB)

	// Unix socket settings for external integrations
	Socket     bool   `yaml:"socket" json:"socket"`           // Enable Unix socket listener
	SocketPath string `yaml:"socket_path" json:"socket_path"` // Path to socket (default: ~/.firebell/firebell.sock)
}

// NotifyConfig defines notification destination and settings.
type NotifyConfig struct {
	Type     string          `yaml:"type" json:"type"` // "slack" or "stdout"
	Slack    SlackConfig     `yaml:"slack,omitempty" json:"slack,omitempty"`
	Webhooks []WebhookConfig `yaml:"webhooks,omitempty" json:"webhooks,omitempty"` // Additional webhook endpoints
}

// WebhookConfig defines a webhook endpoint for notifications.
type WebhookConfig struct {
	URL     string            `yaml:"url" json:"url"`
	Events  []string          `yaml:"events,omitempty" json:"events,omitempty"`   // Event types to send (empty = all)
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"` // Custom HTTP headers
	Timeout int               `yaml:"timeout,omitempty" json:"timeout,omitempty"` // Timeout in seconds (default: 10)
}

// SlackConfig holds Slack-specific notification settings.
type SlackConfig struct {
	Webhook string `yaml:"webhook" json:"webhook"`
}

// AgentsConfig defines which AI agents to monitor and their log paths.
type AgentsConfig struct {
	Enabled []string          `yaml:"enabled,omitempty" json:"enabled,omitempty"` // nil = auto-detect
	Paths   map[string]string `yaml:"paths,omitempty" json:"paths,omitempty"`     // Override default paths
}

// MonitorConfig defines monitoring behavior settings.
type MonitorConfig struct {
	ProcessTracking     bool `yaml:"process_tracking" json:"process_tracking"`
	CompletionDetection bool `yaml:"completion_detection" json:"completion_detection"`
	QuietSeconds        int  `yaml:"quiet_seconds" json:"quiet_seconds"`
	PerInstance         bool `yaml:"per_instance" json:"per_instance"` // Track each instance separately (by log file)
}

// OutputConfig defines notification output formatting.
type OutputConfig struct {
	Verbosity       string `yaml:"verbosity" json:"verbosity"` // "minimal" | "normal" | "verbose"
	IncludeSnippets bool   `yaml:"include_snippets" json:"include_snippets"`
	SnippetLines    int    `yaml:"snippet_lines" json:"snippet_lines"`
}

// AdvancedConfig holds advanced/power-user settings.
// These are typically not changed from defaults.
type AdvancedConfig struct {
	PollIntervalMS int  `yaml:"poll_interval_ms" json:"poll_interval_ms"`
	MaxRecentFiles int  `yaml:"max_recent_files" json:"max_recent_files"`
	WatchDepth     int  `yaml:"watch_depth" json:"watch_depth"`
	ForcePolling   bool `yaml:"force_polling" json:"force_polling"` // Use polling instead of fsnotify
}

// DefaultConfig returns a Config with sensible defaults for v2.0.
func DefaultConfig() *Config {
	return &Config{
		Version: "2",
		Notify: NotifyConfig{
			Type: "slack",
		},
		Agents: AgentsConfig{
			Enabled: nil, // Auto-detect active agents
		},
		Monitor: MonitorConfig{
			ProcessTracking:     true,
			CompletionDetection: true,
			QuietSeconds:        15,
			PerInstance:         true, // Track each instance separately by default
		},
		Output: OutputConfig{
			Verbosity:       "normal",
			IncludeSnippets: true,
			SnippetLines:    12,
		},
		Daemon: DaemonConfig{
			LogRetentionDays: 7,
			EventFile:        true,                    // Enable by default
			EventFileMaxSize: 10 * 1024 * 1024,        // 10MB
			Socket:           false,                   // Disabled by default
		},
		Advanced: AdvancedConfig{
			PollIntervalMS: 800,
			MaxRecentFiles: 3,
			WatchDepth:     4,
		},
	}
}

// PollInterval returns the configured poll interval as a time.Duration.
func (c *Config) PollInterval() time.Duration {
	return time.Duration(c.Advanced.PollIntervalMS) * time.Millisecond
}

// QuietDuration returns the quiet period as a time.Duration.
func (c *Config) QuietDuration() time.Duration {
	return time.Duration(c.Monitor.QuietSeconds) * time.Second
}

// Validate checks that the configuration is valid and returns an error if not.
func (c *Config) Validate() error {
	// Notification validation
	if c.Notify.Type != "slack" && c.Notify.Type != "stdout" {
		return &ValidationError{Field: "notify.type", Message: "must be 'slack' or 'stdout'"}
	}

	if c.Notify.Type == "slack" && c.Notify.Slack.Webhook == "" {
		return &ValidationError{Field: "notify.slack.webhook", Message: "Slack webhook URL is required when type is 'slack'"}
	}

	// Output verbosity validation
	validVerbosity := map[string]bool{"minimal": true, "normal": true, "verbose": true}
	if !validVerbosity[c.Output.Verbosity] {
		return &ValidationError{Field: "output.verbosity", Message: "must be 'minimal', 'normal', or 'verbose'"}
	}

	// Advanced config validation
	if c.Advanced.PollIntervalMS < 100 {
		return &ValidationError{Field: "advanced.poll_interval_ms", Message: "must be at least 100ms"}
	}

	if c.Advanced.MaxRecentFiles < 1 {
		return &ValidationError{Field: "advanced.max_recent_files", Message: "must be at least 1"}
	}

	if c.Monitor.QuietSeconds < 0 {
		return &ValidationError{Field: "monitor.quiet_seconds", Message: "cannot be negative"}
	}

	return nil
}

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return "config validation error: " + e.Field + ": " + e.Message
}
