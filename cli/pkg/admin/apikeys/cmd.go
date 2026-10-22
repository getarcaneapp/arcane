package apikeys

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the parent command for API key operations
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "api-keys",
		Aliases: []string{"apikey", "key"},
		Short:   "Manage API keys",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newCreateCmd())
	command.AddCommand(newGetCmd())
	command.AddCommand(newUpdateCmd())
	command.AddCommand(newDeleteCmd())
	return command
}

func newListCmd() *cobra.Command {
	var opts struct {
		limit int
		start int
		all   bool
	}
	command := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List API keys",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.ApiKey]{
				Resource: "API keys",
				Endpoint: clitypes.ApiKeys(),
				Params:   cmdutil.ListParams{Resource: "apikeys", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "NAME", "DESCRIPTION", "PERMISSIONS", "CREATED", "LAST USED"},
				Row: func(key types.ApiKey) []string {
					lastUsed := "Never"
					if key.LastUsedAt != nil {
						lastUsed = key.LastUsedAt.Format("2006-01-02 15:04")
					}
					return []string{
						key.ID,
						key.Name,
						kit.FromPtr(key.Description),
						strconv.Itoa(len(key.Permissions)),
						key.CreatedAt.Format("2006-01-02 15:04"),
						lastUsed,
					}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of API keys to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return command
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		permissions []string
		description string
		expiresAt   string
	}
	command := &cobra.Command{
		Use:          "create <name>",
		Short:        "Create a new API key",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			grants, grantsErr := permissionGrants(cmd.Context(), c, opts.permissions)
			if grantsErr != nil {
				return grantsErr
			}
			if len(grants) == 0 {
				return errors.New("at least one --permission is required (e.g. --permission containers:list)")
			}
			var expiresAt *time.Time
			if opts.expiresAt != "" {
				parsed, parseErr := time.Parse(time.RFC3339, opts.expiresAt)
				if parseErr != nil {
					return fmt.Errorf("invalid --expires-at format (use RFC3339): %w", parseErr)
				}
				expiresAt = &parsed
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ApiKeyCreatedDto]{
				Method:         http.MethodPost,
				Path:           clitypes.ApiKeys(),
				FailureMessage: "failed to create API key",
				Body: types.ApiKey{
					Name:        args[0],
					Permissions: grants,
					Description: kit.Ternary(opts.description != "", &opts.description, nil),
					ExpiresAt:   expiresAt,
				},
				Render: func(data types.ApiKeyCreatedDto) error {
					output.Success("API key created successfully")
					output.Header("API Key Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					output.KeyValue("Key", data.Key)
					output.KeyValue("Created", data.CreatedAt.Format("2006-01-02 15:04"))
					output.Warning("Store the token securely - it will not be shown again!")
					return nil
				},
			})
		},
	}
	command.Flags().StringVarP(&opts.description, "description", "d", "", "Description for the API key")
	command.Flags().StringVar(&opts.expiresAt, "expires-at", "", "Expiration date (RFC3339 format)")
	command.Flags().StringArrayVarP(&opts.permissions, "permission", "p", nil,
		"Permission to grant as `resource:action[:envId]` (repeatable, required). Omit envId for a global grant. Cannot exceed your own permissions.")
	_ = command.MarkFlagRequired("permission")
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete API key",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete API key %s?", args[0]),
				Path:           clitypes.ApiKey(args[0]),
				FailureMessage: "failed to delete API key",
				SuccessMessage: "API key deleted successfully",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <id>",
		Short:        "Get API key details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ApiKey]{
				Method:         http.MethodGet,
				Path:           clitypes.ApiKey(args[0]),
				FailureMessage: "failed to get API key",
				Render: func(data types.ApiKey) error {
					output.Header("API Key Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					if data.Description != nil {
						output.KeyValue("Description", *data.Description)
					}
					output.KeyValue("Key Prefix", data.KeyPrefix)
					output.KeyValue("Created", data.CreatedAt.Format("2006-01-02 15:04"))
					if data.LastUsedAt != nil {
						output.KeyValue("Last Used", data.LastUsedAt.Format("2006-01-02 15:04"))
					}
					if data.ExpiresAt != nil {
						output.KeyValue("Expires", data.ExpiresAt.Format("2006-01-02 15:04"))
					}
					output.KeyValue("Permissions", strconv.Itoa(len(data.Permissions)))
					if len(data.Permissions) > 0 {
						rows := make([][]string, len(data.Permissions))
						for i, g := range data.Permissions {
							scope := "global"
							if g.EnvironmentID != nil {
								scope = *g.EnvironmentID
							}
							rows[i] = []string{g.Permission, scope}
						}
						if tableErr := output.Table([]string{"PERMISSION", "SCOPE"}, rows); tableErr != nil {
							return tableErr
						}
					}
					return nil
				},
			})
		},
	}
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		name        string
		description string
		expiresAt   string
		permissions []string
	}
	command := &cobra.Command{
		Use:          "update <id>",
		Short:        "Update API key",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			var expiresAt *time.Time
			if opts.expiresAt != "" {
				parsedTime, parseErr := time.Parse(time.RFC3339, opts.expiresAt)
				if parseErr != nil {
					return fmt.Errorf("invalid expires-at format (use RFC3339): %w", parseErr)
				}
				expiresAt = &parsedTime
			}
			permissions, grantsErr := permissionGrants(cmd.Context(), c, opts.permissions)
			if grantsErr != nil {
				return grantsErr
			}

			body := map[string]any{}
			for _, field := range []struct {
				flag  string
				key   string
				value any
			}{
				{flag: "name", key: "name", value: opts.name},
				{flag: "description", key: "description", value: opts.description},
				{flag: "expires-at", key: "expiresAt", value: expiresAt},
				{flag: "permission", key: "permissions", value: permissions},
			} {
				if cmd.Flags().Changed(field.flag) {
					body[field.key] = field.value
				}
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ApiKey]{
				Method:         http.MethodPatch,
				Path:           clitypes.ApiKey(args[0]),
				FailureMessage: "failed to update API key",
				Body:           body,
				Render: func(types.ApiKey) error {
					output.Success("API key updated successfully")
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.name, "name", "", "API key name")
	command.Flags().StringVarP(&opts.description, "description", "d", "", "API key description")
	command.Flags().StringVar(&opts.expiresAt, "expires-at", "", "Expiration date (RFC3339 format)")
	command.Flags().StringArrayVarP(&opts.permissions, "permission", "p", nil,
		"Replace the key's permission grants. Use `resource:action[:envId]` (repeatable). Pass --permission \"\" once to clear all grants.")
	return command
}

func permissionGrants(ctx context.Context, c *client.Client, rawPermissions []string) ([]types.ApiKeyPermissionGrant, error) {
	grants := make([]types.ApiKeyPermissionGrant, 0, len(rawPermissions))
	names := make([]string, 0, len(rawPermissions))
	for _, raw := range rawPermissions {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		parts := strings.Split(token, ":")
		if (len(parts) != 2 && len(parts) != 3) || parts[0] == "" || parts[1] == "" || (len(parts) == 3 && parts[2] == "") {
			return nil, fmt.Errorf("invalid --permission value %q (expected `resource:action[:envId]`)", raw)
		}
		var environmentID *string
		if len(parts) == 3 {
			environmentID = new(parts[2])
		}
		grants = append(grants, types.ApiKeyPermissionGrant{
			Permission:    parts[0] + ":" + parts[1],
			EnvironmentID: environmentID,
		})
		names = append(names, parts[0]+":"+parts[1])
	}
	if err := cmdutil.ValidatePermissions(ctx, c, names); err != nil {
		return nil, err
	}
	return grants, nil
}
