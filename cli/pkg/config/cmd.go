package config

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	"github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the command for managing API configuration
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Annotations: map[string]string{"arcane:scope": cmdutil.ScopeGlobal},
		Use:         "config",
		Short:       "Manage Arcane CLI's Configuration",
	}
	cmd.AddCommand(newConfigShowCmd(), newConfigSetCmd(), newConfigInitCmd(), newConfigBackupCmd(), newConfigPathCmd(), newConfigTestCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			path, _ := config.ConfigPath()
			if cmdutil.JSONOutputEnabled(cmd) {
				cfg.APIKey = maskAPIKey(cfg.APIKey)
				cfg.JWTToken = maskAPIKey(cfg.JWTToken)
				cfg.RefreshToken = maskAPIKey(cfg.RefreshToken)
				return cmdutil.PrintJSON(cfg)
			}
			output.Header("CLI configuration")
			output.KeyValue("Config file", path)
			authPath, authPathErr := config.AuthPath()
			if authPathErr != nil {
				authPath = fmt.Sprintf("(unavailable: %v)", authPathErr)
			}
			output.KeyValue("Auth file", authPath)
			output.KeyValue("Server URL", cmp.Or(cfg.ServerURL, "(not set)"))
			output.KeyValue("API key", maskAPIKey(cfg.APIKey))
			output.KeyValue("Federated audience", cmp.Or(cfg.FederatedAudience, "(not set)"))
			output.KeyValue("Log level", cmp.Or(cfg.LogLevel, "info (default)"))
			output.KeyValue("Update channel", cmp.Or(cfg.CLIUpdateChannel, "(auto)"))
			output.KeyValue("Pagination limit", kit.Ternary(cfg.Pagination.Default.Limit > 0, strconv.Itoa(cfg.Pagination.Default.Limit), "(not set)"))
			if cfg.JWTToken != "" || cfg.RefreshToken != "" {
				output.Header("Session")
				output.KeyValue("Access token", maskAPIKey(cfg.JWTToken))
				output.KeyValue("Refresh token", maskAPIKey(cfg.RefreshToken))
			}
			output.Header("Previews")
			output.KeyValue("newSelfUpdater", strconv.FormatBool(cfg.Previews.NewSelfUpdater))
			output.Header("Pagination resources")
			for _, resource := range types.KnownPaginatedResources {
				limit := cfg.LimitFor(resource)
				explicit := cfg.Pagination.Resources != nil && cfg.Pagination.Resources[resource].Limit > 0
				label := "(inherit)"
				switch {
				case explicit:
					label = strconv.Itoa(limit)
				case limit > 0:
					label = fmt.Sprintf("%d (from global)", limit)
				}
				output.KeyValue(resource, label)
			}
			if cfg.Pagination.Resources != nil {
				extras := make([]string, 0)
				for k, v := range cfg.Pagination.Resources {
					if !slices.Contains(types.KnownPaginatedResources, k) && v.Limit > 0 {
						extras = append(extras, k)
					}
				}
				sort.Strings(extras)
				for _, k := range extras {
					_, _ = fmt.Fprintf(output.Stdout(), "  %-14s %d\n", k+":", cfg.Pagination.Resources[k].Limit)
				}
			}

			if cfg.IsConfigured() {
				_, _ = fmt.Fprintln(output.Stdout(), "\n✓ Configuration is complete")
			} else {
				_, _ = fmt.Fprintln(output.Stdout(), "\n✗ Configuration is incomplete. Run: arcane config set --help")
			}

			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	var opts struct {
		serverURL         string
		apiKey            string
		jwtToken          string
		federatedAudience string
		logLevel          string
		defaultLimit      int
		resourceLimits    []string
	}
	cmd := &cobra.Command{
		Use:   "set [key value]...",
		Short: "Set configuration values",
		Long: `Set configuration values for connecting to an Arcane server.

Preferred syntax (key/value pairs):

Examples:
	arcane config set server-url http://localhost:3553
	arcane config set api-key arc_xxxxxxxxxxxxx
	arcane config set previews.newSelfUpdater true
	arcane config set pagination.default.limit 25
	arcane config set pagination.resources.images.limit 100
	arcane config set server-url http://localhost:3553 api-key arc_xxxxxxxxxxxxx

Legacy flag syntax (flags shown below) is still supported:
	arcane config set --server-url http://localhost:3553
	arcane config set --api-key arc_xxxxxxxxxxxxx
	arcane auth login
	arcane config set --jwt-token eyJhbGciOi...`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			w := output.Stdout()
			if cmdutil.JSONOutputEnabled(cmd) {
				w = io.Discard
			}
			var flagArgs []string
			updatedKeys := make([]string, 0, len(args)/2)
			for i := 0; i < len(args); i += 2 {
				updatedKeys = append(updatedKeys, args[i])
			}
			for _, flag := range []struct{ key, value string }{
				{"server-url", opts.serverURL},
				{"api-key", opts.apiKey},
				{"jwt-token", opts.jwtToken},
				{"federated-audience", opts.federatedAudience},
				{"log-level", opts.logLevel},
			} {
				if flag.value != "" {
					flagArgs = append(flagArgs, flag.key, flag.value)
					updatedKeys = append(updatedKeys, flag.key)
				}
			}
			if cmd.Flags().Changed("default-limit") {
				if opts.defaultLimit < 0 {
					return errors.New("--default-limit must be >= 0")
				}
				flagArgs = append(flagArgs, "default-limit", strconv.Itoa(opts.defaultLimit))
				updatedKeys = append(updatedKeys, "default-limit")
			}
			for _, pair := range opts.resourceLimits {
				flagArgs = append(flagArgs, "resource-limit", pair)
				resource, _, _ := strings.Cut(pair, "=")
				updatedKeys = append(updatedKeys, "pagination.resources."+types.NormalizePaginatedResource(resource)+".limit")
			}

			if setErr := applyConfigSetArgs(cmd.Context(), cfg, args, flagArgs, w); setErr != nil {
				return setErr
			}

			if len(args) == 0 && len(opts.resourceLimits) == 0 && !cmd.Flags().Changed("default-limit") &&
				cmp.Or(opts.serverURL, opts.apiKey, opts.jwtToken, opts.federatedAudience, opts.logLevel) == "" {
				return errors.New("no configuration values provided. Use `arcane config set <key> <value>` (e.g. `arcane config set server-url " +
					"http://localhost:3552`) or legacy flags (--server-url, --api-key, --jwt-token, --log-level, " +
					"--default-limit, --resource-limit)")
			}

			save := config.Save
			if cfg.APIKey == "" && slices.ContainsFunc(updatedKeys, func(key string) bool {
				normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(key)), " ", "")
				return slices.Contains([]string{"jwt-token", "jwt", "jwt_token"}, normalized)
			}) {
				save = config.SaveTokens
			}
			if saveErr := save(cmd.Context(), cfg); saveErr != nil {
				return fmt.Errorf("failed to save config: %w", saveErr)
			}

			path, _ := config.ConfigPath()
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Path        string   `json:"path"`
					UpdatedKeys []string `json:"updatedKeys"`
				}{Path: path, UpdatedKeys: updatedKeys})
			}
			_, _ = fmt.Fprintf(output.Stdout(), "\nConfiguration saved to %s\n", path)

			return nil
		},
	}
	cmd.Flags().StringVar(&opts.serverURL, "server-url", "", "Arcane server URL (e.g., http://localhost:3553)")
	cmd.Flags().StringVar(&opts.apiKey, "api-key", "", "API key for authentication")
	cmd.Flags().StringVar(&opts.jwtToken, "jwt-token", "", "JWT access token for authentication (Bearer token)")
	cmd.Flags().StringVar(&opts.federatedAudience, "federated-audience", "", "Default audience for federated authentication")
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.logLevel, "log-level", "", "", "Default log level", "debug", "info", "warn", "error")
	cmd.Flags().IntVar(&opts.defaultLimit, "default-limit", 0, "Global default list limit for paginated resources (0 clears)")
	cmd.Flags().StringSliceVar(&opts.resourceLimits, "resource-limit", nil, "Per-resource list limit in the form resource=limit (repeatable, 0 clears)")
	return cmd
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.ConfigPath()
			if err != nil {
				return err
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Path string `json:"path"`
				}{Path: path})
			}
			_, _ = fmt.Fprintln(output.Stdout(), path)
			return nil
		},
	}
}

func newConfigTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Test the API connection",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if validateErr := cfg.Validate(); validateErr != nil {
				return validateErr
			}

			if !cmdutil.JSONOutputEnabled(cmd) {
				_, _ = fmt.Fprintf(output.Stdout(), "Testing connection to %s...\n", cfg.ServerURL)
			}

			httpClient := &http.Client{Timeout: 10 * time.Second}
			req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, cfg.ServerURL+"/api/version", http.NoBody)
			if err != nil {
				return fmt.Errorf("failed to create request: %w", err)
			}
			// Prefer JWT bearer if present, else API key.
			if cfg.JWTToken != "" {
				req.Header.Set("Authorization", "Bearer "+cfg.JWTToken)
			} else {
				req.Header.Set("X-Api-Key", cfg.APIKey)
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

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					ServerURL string `json:"serverUrl"`
					Success   bool   `json:"success"`
				}{ServerURL: cfg.ServerURL, Success: true})
			}
			_, _ = fmt.Fprintln(output.Stdout(), "✓ Connection successful!")
			return nil
		},
	}
}

func newConfigInitCmd() *cobra.Command {
	return &cobra.Command{
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
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Path    string `json:"path"`
					Created bool   `json:"created"`
				}{Path: path, Created: created})
			}
			if !created {
				_, _ = fmt.Fprintf(output.Stdout(), "Config file already exists at %s (no changes made)\n", path)
				return nil
			}

			_, _ = fmt.Fprintf(output.Stdout(), "Created default config at %s\n", path)
			_, _ = fmt.Fprintln(output.Stdout(), "Update values with `arcane config set <key> <value>` or run `arcane auth login`.")
			return nil
		},
	}
}

func newConfigBackupCmd() *cobra.Command {
	var opts struct{ auth bool }
	cmd := &cobra.Command{
		Use:          "backup",
		Short:        "Move the current config file to a .bak backup",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		Long: `Move the active config file to <config-path>.bak.

This removes the original config file from its previous path.
With --auth, move ~/.arcanecli/auth.json instead, for example when it is damaged.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pathFn := config.ConfigPath
			if opts.auth {
				pathFn = config.AuthPath
			}
			path, err := pathFn()
			if err != nil {
				return err
			}

			var backupPath string
			var moved bool
			if opts.auth {
				backupPath, moved, err = config.BackupAuthFile(cmd.Context())
			} else {
				backupPath, moved, err = config.BackupFile()
			}
			if err != nil {
				return fmt.Errorf("failed to backup %s: %w", filepath.Base(path), err)
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(struct {
					Path  string `json:"path"`
					Moved bool   `json:"moved"`
				}{Path: cmp.Or(backupPath, path), Moved: moved})
			}
			if !moved {
				_, _ = fmt.Fprintf(output.Stdout(), "No file found at %s (no changes made)\n", path)
				return nil
			}

			_, _ = fmt.Fprintf(output.Stdout(), "Backed up %s to %s\n", filepath.Base(path), backupPath)
			_, _ = fmt.Fprintf(output.Stdout(), "Removed original at %s\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.auth, "auth", false, "Back up the saved sessions file instead of the config")
	return cmd
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

func applyConfigSetArgs(ctx context.Context, cfg *types.Config, args, flags []string, w io.Writer) error {
	if len(args)%2 != 0 {
		return fmt.Errorf("expected key/value pairs, got odd number of arguments (%d). Example: `arcane config set server-url http://localhost:3552`", len(args))
	}
	args = append(slices.Clone(args), flags...)
	// Switch servers before credential keys so tokens attach to the final server regardless of order.
	for _, serverFirst := range []bool{true, false} {
		for i := 0; i < len(args); i += 2 {
			key := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(args[i])), " ", "")
			if slices.Contains([]string{"server-url", "server", "serverurl", "server_url"}, key) != serverFirst {
				continue
			}
			if err := applyConfigSetArg(ctx, cfg, args[i], args[i+1], w); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyConfigSetArg(ctx context.Context, cfg *types.Config, key, value string, w io.Writer) error {
	normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(key)), " ", "")
	switch normalized {
	case "server-url", "server", "serverurl", "server_url":
		if err := config.SwitchServer(ctx, cfg, value); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "Set server_url = %s\n", value)
		return nil
	case "api-key", "apikey", "api_key":
		cfg.APIKey = value
		// If switching to API key auth, clear any existing JWT token.
		cfg.JWTToken = ""
		cfg.RefreshToken = ""
		_, _ = fmt.Fprintf(w, "Set api_key = %s\n", maskAPIKey(value))
		return nil
	case "jwt-token", "jwt", "jwt_token":
		cfg.JWTToken = value
		// If switching to JWT auth, clear any existing API key.
		cfg.APIKey = ""
		cfg.RefreshToken = ""
		_, _ = fmt.Fprintf(w, "Set jwt_token = %s\n", maskAPIKey(value))
		return nil
	case "previews.newselfupdater":
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid previews.newSelfUpdater value %q (expected true or false)", value)
		}
		cfg.Previews.NewSelfUpdater = enabled
		_, _ = fmt.Fprintf(w, "Set previews.newSelfUpdater = %t\n", enabled)
		return nil
	case "federated-audience", "federated_audience", "audience":
		cfg.FederatedAudience = value
		_, _ = fmt.Fprintf(w, "Set federated_audience = %s\n", value)
		return nil
	case "log-level", "loglevel", "log_level":
		cfg.LogLevel = value
		_, _ = fmt.Fprintf(w, "Set log_level = %s\n", value)
		return nil
	case "cli-update-channel", "cli_update_channel", "cli-channel", "channel":
		channel := strings.ToLower(strings.TrimSpace(value))
		if channel != "stable" && channel != "next" {
			return fmt.Errorf("invalid cli update channel %q (expected stable or next)", value)
		}
		cfg.CLIUpdateChannel = channel
		_, _ = fmt.Fprintf(w, "Set cli_update_channel = %s\n", channel)
		return nil
	case "default-limit", "default_limit", "pagination.default.limit":
		limit, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid value %q for key %q (expected a non-negative integer)", value, normalized)
		}
		if limit < 0 {
			return fmt.Errorf("invalid value %q for key %q (must be >= 0)", value, normalized)
		}
		cfg.SetDefaultLimit(limit)
		if limit == 0 {
			_, _ = fmt.Fprintln(w, "Cleared pagination.default.limit")
		} else {
			_, _ = fmt.Fprintf(w, "Set pagination.default.limit = %d\n", limit)
		}
		return nil
	}

	var resource string
	var limit int
	if normalized == "resource-limit" || normalized == "resource_limit" {
		left, right, found := strings.Cut(value, "=")
		resource = types.NormalizePaginatedResource(left)
		parsed, err := strconv.Atoi(strings.TrimSpace(right))
		if !found || resource == "" || !slices.Contains(types.KnownPaginatedResources, resource) || err != nil {
			return fmt.Errorf("invalid value %q for key %q (expected resource=number, resources: %s)", value, key, strings.Join(types.KnownPaginatedResources, ", "))
		}
		if parsed < 0 {
			return fmt.Errorf("invalid value %q for key %q (limit must be >= 0)", value, key)
		}
		limit = parsed
	} else {
		var resourceValue string
		switch {
		case strings.HasPrefix(normalized, "resource-limit.") || strings.HasPrefix(normalized, "resource_limit."):
			resourceValue = strings.TrimPrefix(strings.TrimPrefix(normalized, "resource-limit."), "resource_limit.")
		case strings.HasPrefix(normalized, "pagination.resources.") && strings.HasSuffix(normalized, ".limit"):
			resourceValue = strings.TrimSuffix(strings.TrimPrefix(normalized, "pagination.resources."), ".limit")
		default:
			return fmt.Errorf("unknown config key %q. Supported keys include server-url, api-key, jwt-token, "+
				"federated-audience, log-level, default-limit, resource-limit, and pagination.resources.<resource>.limit", key)
		}
		resource = types.NormalizePaginatedResource(resourceValue)
		if resource == "" || !slices.Contains(types.KnownPaginatedResources, resource) {
			return fmt.Errorf("invalid resource %q for key %q (supported resources: %s)", resourceValue, key, strings.Join(types.KnownPaginatedResources, ", "))
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("invalid value %q for key %q (expected a non-negative integer)", value, key)
		}
		if parsed < 0 {
			return fmt.Errorf("invalid value %q for key %q (must be >= 0)", value, key)
		}
		limit = parsed
	}
	cfg.SetResourceLimit(resource, limit)
	if limit == 0 {
		_, _ = fmt.Fprintf(w, "Cleared pagination.resources.%s.limit\n", resource)
	} else {
		_, _ = fmt.Fprintf(w, "Set pagination.resources.%s.limit = %d\n", resource, limit)
	}
	return nil
}
