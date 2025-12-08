package config

import (
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

	if cfg.Monitor.QuietSeconds != 20 {
		t.Errorf("Expected quiet seconds 20, got %d", cfg.Monitor.QuietSeconds)
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
