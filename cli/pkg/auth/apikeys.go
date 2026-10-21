package auth

import (
	"fmt"
	"net/http"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// apiKeysCmd manages personal keys, which inherit the owner's role permissions.
// Scoped keys are managed through `arcane admin api-keys`.
func newAPIKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "api-keys",
		Aliases: []string{"apikeys", "key"},
		Short:   "Manage your own API keys",
		Long: "Manage the current user's personal API keys. Personal keys inherit the owner's role permissions. Creating and " +
			"deleting them requires a session login (an API key cannot mint or remove other keys).",
	}
	cmd.AddCommand(newAPIKeysListCmd(), newAPIKeysCreateCmd(), newAPIKeysDeleteCmd())
	return cmd
}

func newAPIKeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List your API keys",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.ApiKey]{
				Method:         http.MethodGet,
				Path:           clitypes.AuthMeApiKeys(),
				FailureMessage: "failed to list API keys",
				Render: func(data []types.ApiKey) error {
					if len(data) == 0 {
						output.Info("No API keys found")
						return nil
					}

					headers := []string{"ID", "NAME", "KIND", "PREFIX", "EXPIRES", "LAST USED", "CREATED"}
					rows := make([][]string, len(data))
					for i, key := range data {
						rows[i] = []string{
							key.ID,
							key.Name,
							key.Kind,
							key.KeyPrefix,
							kit.Ternary(key.ExpiresAt != nil, kit.FromPtr(key.ExpiresAt).Format("2006-01-02 15:04"), "never"),
							kit.Ternary(key.LastUsedAt != nil, kit.FromPtr(key.LastUsedAt).Format("2006-01-02 15:04"), "never"),
							key.CreatedAt.Format("2006-01-02 15:04"),
						}
					}
					return output.Table(headers, rows)
				},
			})
		},
	}
}

func newAPIKeysCreateCmd() *cobra.Command {
	var opts struct {
		description string
		expiresAt   string
	}
	cmd := &cobra.Command{
		Use:          "create <name>",
		Short:        "Create a personal API key",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
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
				Method: http.MethodPost,
				Path:   clitypes.AuthMeApiKeys(),
				Body: types.ApiKey{
					Name:        args[0],
					Description: kit.Ternary(cmd.Flags().Changed("description"), &opts.description, nil),
					ExpiresAt:   expiresAt,
				},
				FailureMessage: "failed to create API key",
				Render: func(data types.ApiKeyCreatedDto) error {
					output.Success("API key created")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					output.Warning("The key secret is shown only once — store it now:")
					output.KeyValue("Key", data.Key)
					return nil
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.description, "description", "", "Description of the API key")
	cmd.Flags().StringVar(&opts.expiresAt, "expires-at", "", "Expiration date (RFC3339 format)")
	return cmd
}

func newAPIKeysDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "delete <id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete one of your API keys",
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
				Path:           clitypes.AuthMeApiKey(args[0]),
				FailureMessage: "failed to delete API key",
				SuccessMessage: "API key deleted",
			})
		},
	}
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return cmd
}
