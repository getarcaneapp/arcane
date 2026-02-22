package config

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/cli/internal/config"
	clitypes "github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/spf13/cobra"
)

var (
	setServerURL     string
	setAPIKey        string
	setJWTToken      string
	setEnvironment   string
	setLogLevel      string
	setDefaultLimit  int
	setResourceLimit []string
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
		fmt.Printf("Refresh Token:       %s\n", maskAPIKey(cfg.RefreshToken))
		fmt.Printf("Default Environment: %s\n", maskIfEmpty(cfg.DefaultEnvironment, "0 (local)"))
		fmt.Printf("Log Level:           %s\n", maskIfEmpty(cfg.LogLevel, "info (default)"))
		globalLimit := cfg.Pagination.Default.Limit
		if globalLimit <= 0 {
			globalLimit = cfg.DefaultLimit
		}
		fmt.Printf("Pagination Default:  %s\n", maskIfEmpty(intToString(globalLimit), "(not set)"))

		fmt.Println("\nPagination Resources:")
		printed := 0
		for _, resource := range clitypes.KnownPaginatedResources {
			limit := cfg.LimitFor(resource)
			explicit := cfg.Pagination.Resources != nil && cfg.Pagination.Resources[resource].Limit > 0
			label := "(inherit)"
			if explicit {
				label = strconv.Itoa(limit)
			} else if limit > 0 {
				label = fmt.Sprintf("%d (from global)", limit)
			}
			fmt.Printf("  %-14s %s\n", resource+":", label)
			printed++
		}
		if cfg.Pagination.Resources != nil {
			extras := make([]string, 0)
			for k, v := range cfg.Pagination.Resources {
				known := false
				for _, resource := range clitypes.KnownPaginatedResources {
					if resource == k {
						known = true
						break
					}
				}
				if !known && v.Limit > 0 {
					extras = append(extras, k)
				}
			}
			sort.Strings(extras)
			for _, k := range extras {
				fmt.Printf("  %-14s %d\n", k+":", cfg.Pagination.Resources[k].Limit)
				printed++
			}
		}
		if printed == 0 {
			fmt.Println("  (none)")
		}

		if cfg.IsConfigured() {
			fmt.Println("\n✓ Configuration is complete")
		} else {
			fmt.Println("\n✗ Configuration is incomplete. Run: arcane config set --help")
		}

		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set [key value]...",
	Short: "Set configuration values",
	Long: `Set configuration values for connecting to an Arcane server.

Preferred syntax (key/value pairs):

Examples:
	arcane config set server-url http://localhost:3553
	arcane config set api-key arc_xxxxxxxxxxxxx
	arcane config set pagination.default.limit 25
	arcane config set pagination.resources.images.limit 100
	arcane config set server-url http://localhost:3553 api-key arc_xxxxxxxxxxxxx

Legacy flag syntax (flags shown below) is still supported:
	arcane config set --server-url http://localhost:3553
	arcane config set --api-key arc_xxxxxxxxxxxxx
	arcane auth login
	arcane config set --jwt-token eyJhbGciOi...`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		updated := false
		updatedByArgs, err := applyConfigSetArgs(cfg, args)
		if err != nil {
			return err
		}
		updated = updated || updatedByArgs

		updatedByFlags, err := applyConfigSetFlags(cmd, cfg)
		if err != nil {
			return err
		}
		updated = updated || updatedByFlags

		if !updated {
			return fmt.Errorf("no configuration values provided. Use `arcane config set <key> <value>` (e.g. `arcane config set server-url http://localhost:3552`) or legacy flags (--server-url, --api-key, --jwt-token, --environment, --log-level, --default-limit, --resource-limit)")
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

var configInitCmd = &cobra.Command{
	Use:          "init",
	Short:        "Create a default config file if it does not exist",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	Long: `Create a starter config file with all supported configuration keys.

If the config file already exists, this command is a no-op and does not overwrite it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := config.ConfigPath()
		if err != nil {
			return err
		}

		created, err := config.InitDefaultFile()
		if err != nil {
			return fmt.Errorf("failed to initialize config: %w", err)
		}
		if !created {
			fmt.Printf("Config file already exists at %s (no changes made)\n", path)
			return nil
		}

		fmt.Printf("Created default config at %s\n", path)
		fmt.Println("Update values with `arcane config set <key> <value>` or run `arcane auth login`.")
		return nil
	},
}

var configBackupCmd = &cobra.Command{
	Use:          "backup",
	Short:        "Move the current config file to a .bak backup",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	Long: `Move the active config file to <config-path>.bak.

This removes the original config file from its previous path.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := config.ConfigPath()
		if err != nil {
			return err
		}

		backupPath, moved, err := config.BackupFile()
		if err != nil {
			return fmt.Errorf("failed to backup config: %w", err)
		}
		if !moved {
			fmt.Printf("No config file found at %s (no changes made)\n", path)
			return nil
		}

		fmt.Printf("Backed up config to %s\n", backupPath)
		fmt.Printf("Removed original config at %s\n", path)
		return nil
	},
}

func init() {
	ConfigCmd.AddCommand(configShowCmd)
	ConfigCmd.AddCommand(configSetCmd)
	ConfigCmd.AddCommand(configInitCmd)
	ConfigCmd.AddCommand(configBackupCmd)
	ConfigCmd.AddCommand(configPathCmd)
	ConfigCmd.AddCommand(configTestCmd)

	configSetCmd.Flags().StringVar(&setServerURL, "server-url", "", "Arcane server URL (e.g., http://localhost:3553)")
	configSetCmd.Flags().StringVar(&setAPIKey, "api-key", "", "API key for authentication")
	configSetCmd.Flags().StringVar(&setJWTToken, "jwt-token", "", "JWT access token for authentication (Bearer token)")
	configSetCmd.Flags().StringVar(&setEnvironment, "environment", "", "Default environment ID")
	configSetCmd.Flags().StringVar(&setLogLevel, "log-level", "", "Default log level (debug, info, warn, error)")
	configSetCmd.Flags().IntVar(&setDefaultLimit, "default-limit", 0, "Global default list limit for paginated resources (0 clears)")
	configSetCmd.Flags().StringSliceVar(&setResourceLimit, "resource-limit", nil, "Per-resource list limit in the form resource=limit (repeatable, 0 clears)")
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

func parseResourceLimitPair(pair string) (resource string, limit int, ok bool) {
	left, right, found := strings.Cut(pair, "=")
	if !found {
		return "", 0, false
	}
	resource = clitypes.NormalizePaginatedResource(left)
	if resource == "" {
		return "", 0, false
	}
	known := false
	for _, candidate := range clitypes.KnownPaginatedResources {
		if candidate == resource {
			known = true
			break
		}
	}
	if !known {
		return "", 0, false
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil {
		return "", 0, false
	}
	return resource, parsed, true
}

func applyConfigSetFlags(cmd *cobra.Command, cfg *clitypes.Config) (bool, error) {
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
		cfg.RefreshToken = ""
		fmt.Printf("Set api_key = %s\n", maskAPIKey(setAPIKey))
		updated = true
	}

	if setJWTToken != "" {
		cfg.JWTToken = setJWTToken
		// If switching to JWT auth, clear any existing API key.
		cfg.APIKey = ""
		cfg.RefreshToken = ""
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

	if cmd.Flags().Changed("default-limit") {
		if setDefaultLimit < 0 {
			return false, fmt.Errorf("--default-limit must be >= 0")
		}
		cfg.SetDefaultLimit(setDefaultLimit)
		if setDefaultLimit == 0 {
			fmt.Println("Cleared pagination.default.limit")
		} else {
			fmt.Printf("Set pagination.default.limit = %d\n", setDefaultLimit)
		}
		updated = true
	}

	for _, pair := range setResourceLimit {
		resource, limit, ok := parseResourceLimitPair(pair)
		if !ok {
			return false, fmt.Errorf("invalid --resource-limit %q (expected resource=number, resources: %s)", pair, strings.Join(clitypes.KnownPaginatedResources, ", "))
		}
		if limit < 0 {
			return false, fmt.Errorf("invalid --resource-limit %q (limit must be >= 0)", pair)
		}
		cfg.SetResourceLimit(resource, limit)
		if limit == 0 {
			fmt.Printf("Cleared pagination.resources.%s.limit\n", resource)
		} else {
			fmt.Printf("Set pagination.resources.%s.limit = %d\n", resource, limit)
		}
		updated = true
	}

	return updated, nil
}

func applyConfigSetArgs(cfg *clitypes.Config, args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args)%2 != 0 {
		return false, fmt.Errorf("expected key/value pairs, got odd number of arguments (%d). Example: `arcane config set server-url http://localhost:3552`", len(args))
	}

	updated := false
	for i := 0; i < len(args); i += 2 {
		changed, err := applyConfigSetArg(cfg, args[i], args[i+1])
		if err != nil {
			return false, err
		}
		updated = updated || changed
	}
	return updated, nil
}

func applyConfigSetArg(cfg *clitypes.Config, key, value string) (bool, error) {
	normalized := normalizeConfigKey(key)
	switch normalized {
	case "server-url", "server", "serverurl", "server_url":
		cfg.ServerURL = value
		fmt.Printf("Set server_url = %s\n", value)
		return true, nil
	case "api-key", "apikey", "api_key":
		cfg.APIKey = value
		// If switching to API key auth, clear any existing JWT token.
		cfg.JWTToken = ""
		cfg.RefreshToken = ""
		fmt.Printf("Set api_key = %s\n", maskAPIKey(value))
		return true, nil
	case "jwt-token", "jwt", "jwt_token":
		cfg.JWTToken = value
		// If switching to JWT auth, clear any existing API key.
		cfg.APIKey = ""
		cfg.RefreshToken = ""
		fmt.Printf("Set jwt_token = %s\n", maskAPIKey(value))
		return true, nil
	case "environment", "default-environment", "default_environment":
		cfg.DefaultEnvironment = value
		fmt.Printf("Set default_environment = %s\n", value)
		return true, nil
	case "log-level", "loglevel", "log_level":
		cfg.LogLevel = value
		fmt.Printf("Set log_level = %s\n", value)
		return true, nil
	case "default-limit", "default_limit", "pagination.default.limit":
		limit, err := parseLimitValue(normalized, value)
		if err != nil {
			return false, err
		}
		cfg.SetDefaultLimit(limit)
		if limit == 0 {
			fmt.Println("Cleared pagination.default.limit")
		} else {
			fmt.Printf("Set pagination.default.limit = %d\n", limit)
		}
		return true, nil
	case "resource-limit", "resource_limit":
		resource, limit, ok := parseResourceLimitPair(value)
		if !ok {
			return false, fmt.Errorf("invalid value %q for key %q (expected resource=number, resources: %s)", value, key, strings.Join(clitypes.KnownPaginatedResources, ", "))
		}
		if limit < 0 {
			return false, fmt.Errorf("invalid value %q for key %q (limit must be >= 0)", value, key)
		}
		cfg.SetResourceLimit(resource, limit)
		if limit == 0 {
			fmt.Printf("Cleared pagination.resources.%s.limit\n", resource)
		} else {
			fmt.Printf("Set pagination.resources.%s.limit = %d\n", resource, limit)
		}
		return true, nil
	}

	if strings.HasPrefix(normalized, "resource-limit.") || strings.HasPrefix(normalized, "resource_limit.") {
		resource := strings.TrimPrefix(strings.TrimPrefix(normalized, "resource-limit."), "resource_limit.")
		return applyResourceLimitByKey(cfg, key, resource, value)
	}
	if strings.HasPrefix(normalized, "pagination.resources.") && strings.HasSuffix(normalized, ".limit") {
		resource := strings.TrimSuffix(strings.TrimPrefix(normalized, "pagination.resources."), ".limit")
		return applyResourceLimitByKey(cfg, key, resource, value)
	}

	return false, fmt.Errorf("unknown config key %q. Supported keys include server-url, api-key, jwt-token, environment, log-level, default-limit, resource-limit, and pagination.resources.<resource>.limit", key)
}

func applyResourceLimitByKey(cfg *clitypes.Config, key, resourceValue, limitValue string) (bool, error) {
	resource := clitypes.NormalizePaginatedResource(resourceValue)
	if resource == "" || !isKnownPaginatedResource(resource) {
		return false, fmt.Errorf("invalid resource %q for key %q (supported resources: %s)", resourceValue, key, strings.Join(clitypes.KnownPaginatedResources, ", "))
	}
	limit, err := parseLimitValue(key, limitValue)
	if err != nil {
		return false, err
	}
	cfg.SetResourceLimit(resource, limit)
	if limit == 0 {
		fmt.Printf("Cleared pagination.resources.%s.limit\n", resource)
	} else {
		fmt.Printf("Set pagination.resources.%s.limit = %d\n", resource, limit)
	}
	return true, nil
}

func isKnownPaginatedResource(resource string) bool {
	for _, candidate := range clitypes.KnownPaginatedResources {
		if candidate == resource {
			return true
		}
	}
	return false
}

func parseLimitValue(key, value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("invalid value %q for key %q (expected a non-negative integer)", value, key)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("invalid value %q for key %q (must be >= 0)", value, key)
	}
	return parsed, nil
}

func normalizeConfigKey(key string) string {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.ReplaceAll(k, " ", "")
	return k
}

func intToString(v int) string {
	if v <= 0 {
		return ""
	}
	return strconv.Itoa(v)
}
