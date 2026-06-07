package apikeys

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/apikey"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	startFlag  int
	forceFlag  bool
	jsonOutput bool
)

var (
	apikeyCreatePermissions []string
)

var (
	apikeyUpdateName        string
	apikeyUpdateDescription string
	apikeyUpdateExpiresAt   string
	apikeyUpdatePermissions []string
)

// parsePermissionGrantsInternal turns `--permission` tokens into the wire-format
// grant list. Each token is either `resource:action` (global grant) or
// `resource:action:envId` (env-scoped grant). Anything else is an error so the
// user catches typos before the server rejects them.
func parsePermissionGrantsInternal(tokens []string) ([]apikey.PermissionGrant, error) {
	out := make([]apikey.PermissionGrant, 0, len(tokens))
	for _, raw := range tokens {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		parts := strings.Split(token, ":")
		switch len(parts) {
		case 2:
			out = append(out, apikey.PermissionGrant{Permission: token})
		case 3:
			env := parts[2]
			if parts[0] == "" || parts[1] == "" || env == "" {
				return nil, fmt.Errorf("invalid --permission value %q (expected `resource:action[:envId]`)", raw)
			}
			out = append(out, apikey.PermissionGrant{
				Permission:    parts[0] + ":" + parts[1],
				EnvironmentID: &env,
			})
		default:
			return nil, fmt.Errorf("invalid --permission value %q (expected `resource:action[:envId]`)", raw)
		}
	}
	return out, nil
}

// ApiKeysCmd is the parent command for API key operations
var ApiKeysCmd = &cobra.Command{
	Use:     "api-keys",
	Aliases: []string{"apikey", "keys", "key"},
	Short:   "Manage API keys",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List API keys",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path := types.Endpoints.ApiKeys()
		path, err = cmdutil.ApplyPaginationParams(cmd, path, "apikeys", "limit", limitFlag, 20, "start", startFlag)
		if err != nil {
			return fmt.Errorf("failed to build pagination query: %w", err)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list API keys: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[apikey.ApiKey]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to list API keys: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result)
		}

		headers := []string{"ID", "NAME", "DESCRIPTION", "PERMISSIONS", "CREATED", "LAST USED"}
		rows := make([][]string, len(result.Data))
		for i, key := range result.Data {
			description := ""
			if key.Description != nil {
				description = *key.Description
			}
			lastUsed := "Never"
			if key.LastUsedAt != nil {
				lastUsed = key.LastUsedAt.Format("2006-01-02 15:04")
			}
			rows[i] = []string{
				key.ID,
				key.Name,
				description,
				strconv.Itoa(len(key.Permissions)),
				key.CreatedAt.Format("2006-01-02 15:04"),
				lastUsed,
			}
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "API keys")
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:          "create <name>",
	Short:        "Create a new API key",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		description, _ := cmd.Flags().GetString("description")

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		grants, err := parsePermissionGrantsInternal(apikeyCreatePermissions)
		if err != nil {
			return err
		}
		if len(grants) == 0 {
			return errors.New("at least one --permission is required (e.g. --permission containers:list)")
		}
		createReq := apikey.CreateApiKey{
			Name:        args[0],
			Permissions: grants,
		}
		if description != "" {
			createReq.Description = &description
		}
		if expiresAtRaw, _ := cmd.Flags().GetString("expires-at"); expiresAtRaw != "" {
			parsed, err := time.Parse(time.RFC3339, expiresAtRaw)
			if err != nil {
				return fmt.Errorf("invalid --expires-at format (use RFC3339): %w", err)
			}
			createReq.ExpiresAt = &parsed
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.ApiKeys(), createReq)
		if err != nil {
			return fmt.Errorf("failed to create API key: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[apikey.ApiKeyCreatedDto]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to create API key: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("API key created successfully")
		output.Header("API Key Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("Key", result.Data.Key)
		output.KeyValue("Created", result.Data.CreatedAt.Format("2006-01-02 15:04"))
		output.Warning("Store the token securely - it will not be shown again!")
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete API key",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete API key %s?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.Endpoints.ApiKey(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete API key: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete API key: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := cmdutil.DecodeJSON(resp, &result); err != nil {
				return fmt.Errorf("failed to delete API key: %w", err)
			}
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("API key deleted successfully")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <id>",
	Short:        "Get API key details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.ApiKey(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get API key: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[apikey.ApiKey]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to get API key: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("API Key Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		if result.Data.Description != nil {
			output.KeyValue("Description", *result.Data.Description)
		}
		output.KeyValue("Key Prefix", result.Data.KeyPrefix)
		output.KeyValue("Created", result.Data.CreatedAt.Format("2006-01-02 15:04"))
		if result.Data.LastUsedAt != nil {
			output.KeyValue("Last Used", result.Data.LastUsedAt.Format("2006-01-02 15:04"))
		}
		if result.Data.ExpiresAt != nil {
			output.KeyValue("Expires", result.Data.ExpiresAt.Format("2006-01-02 15:04"))
		}
		output.KeyValue("Permissions", strconv.Itoa(len(result.Data.Permissions)))
		if len(result.Data.Permissions) > 0 {
			rows := make([][]string, len(result.Data.Permissions))
			for i, g := range result.Data.Permissions {
				scope := "global"
				if g.EnvironmentID != nil {
					scope = *g.EnvironmentID
				}
				rows[i] = []string{g.Permission, scope}
			}
			output.Table([]string{"PERMISSION", "SCOPE"}, rows)
		}
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <id>",
	Short:        "Update API key",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		var req apikey.UpdateApiKey
		if cmd.Flags().Changed("name") {
			req.Name = &apikeyUpdateName
		}
		if cmd.Flags().Changed("description") {
			req.Description = &apikeyUpdateDescription
		}
		if cmd.Flags().Changed("expires-at") && apikeyUpdateExpiresAt != "" {
			parsedTime, err := time.Parse(time.RFC3339, apikeyUpdateExpiresAt)
			if err != nil {
				return fmt.Errorf("invalid expires-at format (use RFC3339): %w", err)
			}
			req.ExpiresAt = &parsedTime
		}
		if cmd.Flags().Changed("permission") {
			grants, err := parsePermissionGrantsInternal(apikeyUpdatePermissions)
			if err != nil {
				return err
			}
			// Allow `--permission ""` once to clear all grants. Otherwise
			// the parsed slice replaces the key's permission set entirely.
			req.Permissions = grants
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.ApiKey(args[0]), req)
		if err != nil {
			return fmt.Errorf("failed to update API key: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to update API key: %w", err)
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := cmdutil.DecodeJSON(resp, &result); err != nil {
				return fmt.Errorf("failed to update API key: %w", err)
			}
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("API key updated successfully")
		return nil
	},
}

func init() {
	ApiKeysCmd.AddCommand(listCmd)
	ApiKeysCmd.AddCommand(createCmd)
	ApiKeysCmd.AddCommand(getCmd)
	ApiKeysCmd.AddCommand(updateCmd)
	ApiKeysCmd.AddCommand(deleteCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of API keys to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, "Offset for pagination")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Create command flags
	createCmd.Flags().StringP("description", "d", "", "Description for the API key")
	createCmd.Flags().String("expires-at", "", "Expiration date (RFC3339 format)")
	createCmd.Flags().StringArrayVarP(&apikeyCreatePermissions, "permission", "p", nil,
		"Permission to grant as `resource:action[:envId]` (repeatable, required). Omit envId for a global grant. Cannot exceed your own permissions.")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("permission")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Update command flags
	updateCmd.Flags().StringVar(&apikeyUpdateName, "name", "", "API key name")
	updateCmd.Flags().StringVarP(&apikeyUpdateDescription, "description", "d", "", "API key description")
	updateCmd.Flags().StringVar(&apikeyUpdateExpiresAt, "expires-at", "", "Expiration date (RFC3339 format)")
	updateCmd.Flags().StringArrayVarP(&apikeyUpdatePermissions, "permission", "p", nil,
		"Replace the key's permission grants. Use `resource:action[:envId]` (repeatable). Pass --permission \"\" once to clear all grants.")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
