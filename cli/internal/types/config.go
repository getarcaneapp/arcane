package types

import "fmt"

// Config holds the CLI configuration for connecting to an Arcane server.
// It is persisted to disk as YAML and loaded on each CLI invocation.
type Config struct {
	// ServerURL is the base URL of the Arcane server (e.g., http://localhost:3552)
	ServerURL string `yaml:"server_url"`
	// APIKey is the API key for authentication
	APIKey string `yaml:"api_key"`
	// DefaultEnvironment is the default environment ID to use
	DefaultEnvironment string `yaml:"default_environment,omitempty"`
	// LogLevel is the logging level (debug, info, warn, error, fatal, panic)
	LogLevel string `yaml:"log_level,omitempty"`
}

// Validate checks if the configuration has all required fields set.
// It returns an error with instructions if ServerURL or APIKey is missing.
// This should be called before using the config to make API requests.
func (c *Config) Validate() error {
	if c.ServerURL == "" {
		return fmt.Errorf("server_url is not configured. Run: arcane api config set --server-url <url>")
	}
	if c.APIKey == "" {
		return fmt.Errorf("api_key is not configured. Run: arcane api config set --api-key <key>")
	}
	return nil
}

// IsConfigured returns true if both ServerURL and APIKey are set.
// This is a quick check to determine if the CLI has been configured
// without triggering validation errors.
func (c *Config) IsConfigured() bool {
	return c.ServerURL != "" && c.APIKey != ""
}
