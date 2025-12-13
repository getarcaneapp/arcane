package config

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/config"
)

var (
	setServerURL   string
	setAPIKey      string
	setJWTToken    string
	setEnvironment string
	setLogLevel    string
)

// ConfigCmd is the command for managing API configuration
var ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage Arcane CLI's Configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		path, _ := config.ConfigPath()
		fmt.Printf("Config file: %s\n\n", path)
		fmt.Printf("Server URL:          %s\n", maskIfEmpty(cfg.ServerURL, "(not set)"))
		fmt.Printf("API Key:             %s\n", maskAPIKey(cfg.APIKey))
		fmt.Printf("JWT Token:           %s\n", maskAPIKey(cfg.JWTToken))
		fmt.Printf("Default Environment: %s\n", maskIfEmpty(cfg.DefaultEnvironment, "0 (local)"))
		fmt.Printf("Log Level:           %s\n", maskIfEmpty(cfg.LogLevel, "info (default)"))

		if cfg.IsConfigured() {
			fmt.Println("\n✓ Configuration is complete")
		} else {
			fmt.Println("\n✗ Configuration is incomplete. Run: arcane config set --help")
		}

		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set configuration values",
	Long: `Set configuration values for connecting to an Arcane server.

Examples:
	arcane config set --server-url http://localhost:3553
	arcane config set --api-key arc_xxxxxxxxxxxxx
	arcane auth login --username admin
	arcane config set --jwt-token eyJhbGciOi...`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		updated := false

		if setServerURL != "" {
			cfg.ServerURL = setServerURL
			fmt.Printf("Set server_url = %s\n", setServerURL)
			updated = true
		}

		if setAPIKey != "" {
			cfg.APIKey = setAPIKey
			// If switching to API key auth, clear any existing JWT token.
			cfg.JWTToken = ""
			fmt.Printf("Set api_key = %s\n", maskAPIKey(setAPIKey))
			updated = true
		}

		if setJWTToken != "" {
			cfg.JWTToken = setJWTToken
			// If switching to JWT auth, clear any existing API key.
			cfg.APIKey = ""
			fmt.Printf("Set jwt_token = %s\n", maskAPIKey(setJWTToken))
			updated = true
		}

		if setEnvironment != "" {
			cfg.DefaultEnvironment = setEnvironment
			fmt.Printf("Set default_environment = %s\n", setEnvironment)
			updated = true
		}

		if setLogLevel != "" {
			cfg.LogLevel = setLogLevel
			fmt.Printf("Set log_level = %s\n", setLogLevel)
			updated = true
		}

		if !updated {
			return fmt.Errorf("no configuration values provided. Use --server-url, --api-key, --jwt-token, --environment, or --log-level")
		}

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		path, _ := config.ConfigPath()
		fmt.Printf("\nConfiguration saved to %s\n", path)

		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file path",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := config.ConfigPath()
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	},
}

var configTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test the API connection",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if err := cfg.Validate(); err != nil {
			return err
		}

		fmt.Printf("Testing connection to %s...\n", cfg.ServerURL)

		// Test connection directly without importing client to avoid circular import
		httpClient := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, cfg.ServerURL+"/api/version", nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		// Prefer JWT bearer if present, else API key.
		if cfg.JWTToken != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.JWTToken)
		} else {
			req.Header.Set("X-API-KEY", cfg.APIKey)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("connection test failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("connection test failed with status %d: %s", resp.StatusCode, string(body))
		}

		fmt.Println("✓ Connection successful!")
		return nil
	},
}

func init() {
	ConfigCmd.AddCommand(configShowCmd)
	ConfigCmd.AddCommand(configSetCmd)
	ConfigCmd.AddCommand(configPathCmd)
	ConfigCmd.AddCommand(configTestCmd)

	configSetCmd.Flags().StringVar(&setServerURL, "server-url", "", "Arcane server URL (e.g., http://localhost:3553)")
	configSetCmd.Flags().StringVar(&setAPIKey, "api-key", "", "API key for authentication")
	configSetCmd.Flags().StringVar(&setJWTToken, "jwt-token", "", "JWT access token for authentication (Bearer token)")
	configSetCmd.Flags().StringVar(&setEnvironment, "environment", "", "Default environment ID")
	configSetCmd.Flags().StringVar(&setLogLevel, "log-level", "", "Default log level (debug, info, warn, error)")
}

func maskIfEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func maskAPIKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
