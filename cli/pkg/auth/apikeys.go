package auth

import (
	"fmt"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	apikeytypes "github.com/getarcaneapp/arcane/types/v2/apikey"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/spf13/cobra"
)

var (
	apiKeyForceFlag         bool
	apiKeyCreateDescription string
	apiKeyCreateExpiresAt   string
)

// apiKeysCmd is the parent command for the caller's own (personal) API keys.
// Personal keys inherit the owner's role permissions; scoped keys with
// granular grants are managed via `arcane admin apikeys`.
var apiKeysCmd = &cobra.Command{
	Use:     "keys",
	Aliases: []string{"apikeys", "key"},
	Short:   "Manage your own API keys",
	Long: "Manage the current user's personal API keys. Personal keys inherit " +
		"the owner's role permissions. Creating and deleting them requires a " +
		"session login (an API key cannot mint or remove other keys).",
}

var apiKeysListCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List your API keys",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.AuthMeApiKeys())
		if err != nil {
			return errors.WrapIf(err, "failed to list API keys")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]apikeytypes.ApiKey]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return errors.WrapIf(err, "failed to list API keys")
		}

		if cmdutil.JSONOutputEnabled(cmd) || jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		if len(result.Data) == 0 {
			output.Info("No API keys found")
			return nil
		}

		headers := []string{"ID", "NAME", "KIND", "PREFIX", "EXPIRES", "LAST USED", "CREATED"}
		rows := make([][]string, len(result.Data))
		for i, key := range result.Data {
			expires := "never"
			if key.ExpiresAt != nil {
				expires = key.ExpiresAt.Format("2006-01-02 15:04")
			}
			lastUsed := "never"
			if key.LastUsedAt != nil {
				lastUsed = key.LastUsedAt.Format("2006-01-02 15:04")
			}
			rows[i] = []string{
				key.ID,
				key.Name,
				key.Kind,
				key.KeyPrefix,
				expires,
				lastUsed,
				key.CreatedAt.Format("2006-01-02 15:04"),
			}
		}
		output.Table(headers, rows)
		return nil
	},
}

var apiKeysCreateCmd = &cobra.Command{
	Use:          "create <name>",
	Short:        "Create a personal API key",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		req := apikeytypes.CreateUserApiKey{Name: args[0]}
		if cmd.Flags().Changed("description") {
			req.Description = &apiKeyCreateDescription
		}
		if apiKeyCreateExpiresAt != "" {
			parsed, err := time.Parse(time.RFC3339, apiKeyCreateExpiresAt)
			if err != nil {
				return errors.WrapIf(err, "invalid --expires-at format (use RFC3339)")
			}
			req.ExpiresAt = &parsed
		}

		resp, err := c.Post(cmd.Context(), types.AuthMeApiKeys(), req)
		if err != nil {
			return errors.WrapIf(err, "failed to create API key")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[apikeytypes.ApiKeyCreatedDto]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return errors.WrapIf(err, "failed to create API key")
		}

		if cmdutil.JSONOutputEnabled(cmd) || jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("API key created")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		output.Warning("The key secret is shown only once — store it now:")
		output.KeyValue("Key", result.Data.Key)
		return nil
	},
}

var apiKeysDeleteCmd = &cobra.Command{
	Use:          "delete <id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete one of your API keys",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !apiKeyForceFlag {
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

		resp, err := c.Delete(cmd.Context(), types.AuthMeApiKey(args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to delete API key")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete API key")
		}

		output.Success("API key deleted")
		return nil
	},
}

func init() {
	AuthCmd.AddCommand(apiKeysCmd)
	apiKeysCmd.AddCommand(apiKeysListCmd)
	apiKeysCmd.AddCommand(apiKeysCreateCmd)
	apiKeysCmd.AddCommand(apiKeysDeleteCmd)

	apiKeysListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	apiKeysCreateCmd.Flags().StringVar(&apiKeyCreateDescription, "description", "", "Description of the API key")
	apiKeysCreateCmd.Flags().StringVar(&apiKeyCreateExpiresAt, "expires-at", "", "Expiration date (RFC3339 format)")
	apiKeysCreateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	apiKeysDeleteCmd.Flags().BoolVarP(&apiKeyForceFlag, "force", "f", false, "Force deletion without confirmation")
}
