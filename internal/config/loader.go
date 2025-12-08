package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath returns the default configuration file path.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".firebell", "config.yaml")
}

// DefaultConfigDir returns the default configuration directory.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".firebell")
}

// Load loads configuration from the specified path, with auto-detection of format.
// If path doesn't exist, returns default config.
// Supports both v2 YAML and v1 JSON (with migration warnings).
func Load(path string) (*Config, error) {
	// If no path specified, use default
	if path == "" {
		path = DefaultConfigPath()
	}

	// If file doesn't exist, return default config
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Try v2 YAML first
	cfg, err := parseV2YAML(data)
	if err == nil {
		if verr := cfg.Validate(); verr != nil {
			return nil, verr
		}
		return cfg, nil
	}

	// Fallback to v1 JSON
	cfg, err = parseV1JSON(data)
	if err == nil {
		fmt.Fprintln(os.Stderr, "WARNING: v1 config detected at", path)
		fmt.Fprintln(os.Stderr, "Run 'firebell --setup' to migrate to v2 YAML format")
		fmt.Fprintln(os.Stderr, "")

		if verr := cfg.Validate(); verr != nil {
			return nil, verr
		}
		return cfg, nil
	}

	return nil, fmt.Errorf("invalid config format (not v2 YAML or v1 JSON): %w", err)
}

// Save writes the configuration to the specified path in YAML format.
func Save(cfg *Config, path string) error {
	if path == "" {
		path = DefaultConfigPath()
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write with restricted permissions
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// parseV2YAML attempts to parse data as v2 YAML format.
func parseV2YAML(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Check version field to confirm it's v2
	if cfg.Version == "" {
		cfg.Version = "2" // Default if missing
	}

	return &cfg, nil
}

// parseV1JSON attempts to parse data as v1 JSON format and migrate to v2.
func parseV1JSON(data []byte) (*Config, error) {
	var v1 struct {
		Webhook string `json:"webhook"`
	}

	if err := json.Unmarshal(data, &v1); err != nil {
		return nil, err
	}

	// Migrate v1 to v2 format
	cfg := DefaultConfig()
	cfg.Notify.Type = "slack"
	cfg.Notify.Slack.Webhook = v1.Webhook

	return cfg, nil
}

// MigrateConfig migrates a v1 JSON config to v2 YAML format.
// It reads from the old JSON path and writes to the new YAML path.
func MigrateConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	oldPath := filepath.Join(home, ".firebell", "config.json")
	newPath := DefaultConfigPath()

	// Check if old config exists
	data, err := os.ReadFile(oldPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("no v1 config found at %s", oldPath)
	}
	if err != nil {
		return fmt.Errorf("failed to read v1 config: %w", err)
	}

	// Parse v1 JSON
	cfg, err := parseV1JSON(data)
	if err != nil {
		return fmt.Errorf("failed to parse v1 config: %w", err)
	}

	// Check if new config already exists
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("v2 config already exists at %s (remove it first to re-migrate)", newPath)
	}

	// Save as v2 YAML
	if err := Save(cfg, newPath); err != nil {
		return fmt.Errorf("failed to save v2 config: %w", err)
	}

	fmt.Printf("Migrated config from %s to %s\n", oldPath, newPath)
	fmt.Println()
	fmt.Println("You can now delete the old config:")
	fmt.Printf("  rm %s\n", oldPath)

	return nil
}
