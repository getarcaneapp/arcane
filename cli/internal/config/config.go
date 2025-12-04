// Package config handles CLI configuration loading and persistence.
//
// Configuration is stored in a YAML file at ~/.config/arcanecli.yml.
// This package provides functions to load, save, and access configuration
// values including the server URL, API key, and default environment.
//
// # Configuration File
//
// The configuration file uses the following format:
//
//	server_url: https://your-server.com
//	api_key: your-api-key
//	default_environment: "0"
//	log_level: info
//
// # Version Information
//
// Version and Revision variables are set at build time via ldflags:
//
//	go build -ldflags "-X go.getarcane.app/cli/internal/config.Version=1.0.0"
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"go.getarcane.app/cli/internal/types"
	"gopkg.in/yaml.v3"
)

// Version and build information - set via ldflags at build time
var (
	Version  = "dev"
	Revision = "unknown"
)

const (
	configFileName = "arcanecli.yml"
)

// DefaultConfig returns a Config with sensible default values.
// The defaults are:
//   - ServerURL: http://localhost:3552
//   - DefaultEnvironment: "0"
//   - LogLevel: "info"
func DefaultConfig() *types.Config {
	return &types.Config{
		ServerURL:          "http://localhost:3552",
		DefaultEnvironment: "0",
		LogLevel:           "info",
	}
}

// ConfigPath returns the absolute path to the configuration file.
// The config file is located at ~/.config/arcanecli.yml.
// Returns an error if the user's home directory cannot be determined.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, ".config", configFileName), nil
}

// Load reads the configuration from disk and returns it.
// If the config file does not exist, default values are returned.
// Returns an error if the file exists but cannot be read or parsed.
func Load() (*types.Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg types.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// Save writes the configuration to disk.
// The config directory is created if it does not exist.
// The file is created with 0600 permissions for security.
func Save(c *types.Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	// Ensure the config directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
