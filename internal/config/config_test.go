package config

import (
	"flag"
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Check version
	if cfg.Version != "2" {
		t.Errorf("Expected version '2', got '%s'", cfg.Version)
	}

	// Check defaults
	if cfg.Notify.Type != "slack" {
		t.Errorf("Expected notify type 'slack', got '%s'", cfg.Notify.Type)
	}

	if !cfg.Monitor.ProcessTracking {
		t.Error("Expected process tracking to be enabled by default")
	}

	if !cfg.Monitor.CompletionDetection {
		t.Error("Expected completion detection to be enabled by default")
	}

	if cfg.Monitor.QuietSeconds != 15 {
		t.Errorf("Expected quiet seconds 15, got %d", cfg.Monitor.QuietSeconds)
	}

	if cfg.Output.Verbosity != "normal" {
		t.Errorf("Expected verbosity 'normal', got '%s'", cfg.Output.Verbosity)
	}

	if !cfg.Output.IncludeSnippets {
		t.Error("Expected snippets to be included by default")
	}

	if cfg.Advanced.PollIntervalMS != 800 {
		t.Errorf("Expected poll interval 800ms, got %d", cfg.Advanced.PollIntervalMS)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: &Config{
				Notify: NotifyConfig{Type: "stdout"},
				Output: OutputConfig{Verbosity: "normal"},
				Advanced: AdvancedConfig{
					PollIntervalMS: 800,
					MaxRecentFiles: 3,
				},
				Monitor: MonitorConfig{QuietSeconds: 20},
			},
			wantErr: false,
		},
		{
			name: "invalid notify type",
			cfg: &Config{
				Notify: NotifyConfig{Type: "invalid"},
				Output: OutputConfig{Verbosity: "normal"},
				Advanced: AdvancedConfig{
					PollIntervalMS: 800,
					MaxRecentFiles: 3,
				},
				Monitor: MonitorConfig{QuietSeconds: 20},
			},
			wantErr: true,
			errMsg:  "notify.type",
		},
		{
			name: "missing slack webhook",
			cfg: &Config{
				Notify: NotifyConfig{
					Type:  "slack",
					Slack: SlackConfig{Webhook: ""},
				},
				Output: OutputConfig{Verbosity: "normal"},
				Advanced: AdvancedConfig{
					PollIntervalMS: 800,
					MaxRecentFiles: 3,
				},
				Monitor: MonitorConfig{QuietSeconds: 20},
			},
			wantErr: true,
			errMsg:  "webhook",
		},
		{
			name: "invalid verbosity",
			cfg: &Config{
				Notify:  NotifyConfig{Type: "stdout"},
				Output:  OutputConfig{Verbosity: "invalid"},
				Advanced: AdvancedConfig{
					PollIntervalMS: 800,
					MaxRecentFiles: 3,
				},
				Monitor: MonitorConfig{QuietSeconds: 20},
			},
			wantErr: true,
			errMsg:  "verbosity",
		},
		{
			name: "poll interval too low",
			cfg: &Config{
				Notify: NotifyConfig{Type: "stdout"},
				Output: OutputConfig{Verbosity: "normal"},
				Advanced: AdvancedConfig{
					PollIntervalMS: 50,
					MaxRecentFiles: 3,
				},
				Monitor: MonitorConfig{QuietSeconds: 20},
			},
			wantErr: true,
			errMsg:  "poll_interval",
		},
		{
			name: "max_recent_files too low",
			cfg: &Config{
				Notify: NotifyConfig{Type: "stdout"},
				Output: OutputConfig{Verbosity: "normal"},
				Advanced: AdvancedConfig{
					PollIntervalMS: 800,
					MaxRecentFiles: 0,
				},
				Monitor: MonitorConfig{QuietSeconds: 20},
			},
			wantErr: true,
			errMsg:  "max_recent_files",
		},
		{
			name: "negative quiet_seconds",
			cfg: &Config{
				Notify: NotifyConfig{Type: "stdout"},
				Output: OutputConfig{Verbosity: "normal"},
				Advanced: AdvancedConfig{
					PollIntervalMS: 800,
					MaxRecentFiles: 3,
				},
				Monitor: MonitorConfig{QuietSeconds: -1},
			},
			wantErr: true,
			errMsg:  "quiet_seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if verr, ok := err.(*ValidationError); ok {
					if !contains(verr.Field, tt.errMsg) {
						t.Errorf("Expected error field to contain '%s', got '%s'", tt.errMsg, verr.Field)
					}
				}
			}
		})
	}
}

func TestPollInterval(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Advanced.PollIntervalMS = 1000

	interval := cfg.PollInterval()
	if interval.Milliseconds() != 1000 {
		t.Errorf("Expected 1000ms, got %dms", interval.Milliseconds())
	}
}

func TestQuietDuration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Monitor.QuietSeconds = 30

	duration := cfg.QuietDuration()
	if duration.Seconds() != 30 {
		t.Errorf("Expected 30s, got %fs", duration.Seconds())
	}
}

func contains(s, substr string) bool {
	// Simple substring check
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestParseFlags(t *testing.T) {
	// Save original args and restore after test
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	tests := []struct {
		name     string
		args     []string
		setupFn  func() *Flags
		verifyFn func(t *testing.T, f *Flags)
	}{
		{
			name: "default flags",
			args: []string{"firebell"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if f.ConfigPath != "" {
					t.Errorf("Expected empty ConfigPath, got %q", f.ConfigPath)
				}
				if f.Setup || f.Check || f.Stdout || f.Verbose || f.Version {
					t.Error("Expected default bool flags to be false")
				}
			},
		},
		{
			name: "with config flag",
			args: []string{"firebell", "--config", "/path/to/config.yaml"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if f.ConfigPath != "/path/to/config.yaml" {
					t.Errorf("Expected ConfigPath=/path/to/config.yaml, got %q", f.ConfigPath)
				}
			},
		},
		{
			name: "with stdout flag",
			args: []string{"firebell", "--stdout"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.Stdout {
					t.Error("Expected Stdout to be true")
				}
			},
		},
		{
			name: "with verbose flag",
			args: []string{"firebell", "--verbose"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.Verbose {
					t.Error("Expected Verbose to be true")
				}
			},
		},
		{
			name: "with agent flag",
			args: []string{"firebell", "--agent", "claude"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if f.Agent != "claude" {
					t.Errorf("Expected Agent=claude, got %q", f.Agent)
				}
			},
		},
		{
			name: "start subcommand",
			args: []string{"firebell", "start"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.DaemonStart {
					t.Error("Expected DaemonStart to be true")
				}
			},
		},
		{
			name: "stop subcommand",
			args: []string{"firebell", "stop"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.DaemonStop {
					t.Error("Expected DaemonStop to be true")
				}
			},
		},
		{
			name: "restart subcommand",
			args: []string{"firebell", "restart"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.DaemonRestart {
					t.Error("Expected DaemonRestart to be true")
				}
			},
		},
		{
			name: "status subcommand",
			args: []string{"firebell", "status"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.DaemonStatus {
					t.Error("Expected DaemonStatus to be true")
				}
			},
		},
		{
			name: "logs subcommand",
			args: []string{"firebell", "logs"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.DaemonLogs {
					t.Error("Expected DaemonLogs to be true")
				}
			},
		},
		{
			name: "logs subcommand with follow",
			args: []string{"firebell", "logs", "-f"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.DaemonLogs {
					t.Error("Expected DaemonLogs to be true")
				}
				if !f.DaemonFollow {
					t.Error("Expected DaemonFollow to be true")
				}
			},
		},
		{
			name: "wrap subcommand with command",
			args: []string{"firebell", "wrap", "--", "claude"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.Wrap {
					t.Error("Expected Wrap to be true")
				}
				if len(f.WrapArgs) != 1 || f.WrapArgs[0] != "claude" {
					t.Errorf("Expected WrapArgs=[claude], got %v", f.WrapArgs)
				}
				if f.WrapName != "claude" {
					t.Errorf("Expected default WrapName=claude, got %q", f.WrapName)
				}
			},
		},
		{
			name: "wrap subcommand with name flag",
			args: []string{"firebell", "wrap", "--name", "MyAI", "--", "claude"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.Wrap {
					t.Error("Expected Wrap to be true")
				}
				if f.WrapName != "MyAI" {
					t.Errorf("Expected WrapName=MyAI, got %q", f.WrapName)
				}
			},
		},
		{
			name: "wrap subcommand without -- separator",
			args: []string{"firebell", "wrap", "claude"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.Wrap {
					t.Error("Expected Wrap to be true")
				}
			},
		},
		{
			name: "events subcommand",
			args: []string{"firebell", "events"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.Events {
					t.Error("Expected Events to be true")
				}
			},
		},
		{
			name: "events subcommand with follow",
			args: []string{"firebell", "events", "-f"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.Events {
					t.Error("Expected Events to be true")
				}
				if !f.EventsFollow {
					t.Error("Expected EventsFollow to be true")
				}
			},
		},
		{
			name: "listen subcommand",
			args: []string{"firebell", "listen"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.Listen {
					t.Error("Expected Listen to be true")
				}
			},
		},
		{
			name: "listen subcommand with json",
			args: []string{"firebell", "listen", "--json"},
			setupFn: func() *Flags {
				return ParseFlags()
			},
			verifyFn: func(t *testing.T, f *Flags) {
				if !f.Listen {
					t.Error("Expected Listen to be true")
				}
				if !f.ListenJSON {
					t.Error("Expected ListenJSON to be true")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flag set
			flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

			os.Args = tt.args
			f := tt.setupFn()
			tt.verifyFn(t, f)
		})
	}
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{
		Field:   "test.field",
		Message: "test error message",
	}

	if err.Error() == "" {
		t.Error("Expected Error() to return non-empty string")
	}

	if err.Field != "test.field" {
		t.Errorf("Expected Field=test.field, got %q", err.Field)
	}

	if err.Message != "test error message" {
		t.Errorf("Expected Message=test error message, got %q", err.Message)
	}
}
