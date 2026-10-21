// Package oidcmappings manages OIDC claim-to-role mappings.
package oidcmappings

import (
	"fmt"
	"net/http"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command is the parent command for OIDC role mapping operations.
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "oidc",
		Aliases: []string{"oidc-mappings", "oidc-mapping"},
		Short:   "Manage OIDC group → role mappings",
		Long: "Manage OIDC group → role mappings. On every OIDC login, Arcane looks up the user's groups claim and applies " +
			"the matching mappings as source='oidc' role assignments. The groups claim itself is configured via the " +
			"oidcGroupsClaim setting (default `groups`).",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newCreateCmd())
	command.AddCommand(newUpdateCmd())
	command.AddCommand(newDeleteCmd())
	return command
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List OIDC role mappings",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.OidcRoleMapping]{
				Method:         http.MethodGet,
				Path:           clitypes.OidcRoleMappings(),
				FailureMessage: "failed to list OIDC mappings",
				Render: func(data []types.OidcRoleMapping) error {
					if len(data) == 0 {
						output.Info("No OIDC role mappings configured")
						return nil
					}

					headers := []string{"ID", "CLAIM VALUE", "ROLE", "SCOPE"}
					rows := make([][]string, len(data))
					for i, m := range data {
						scope := "global"
						if m.EnvironmentID != nil {
							scope = *m.EnvironmentID
						}
						rows[i] = []string{m.ID, m.ClaimValue, m.RoleID, scope}
					}
					return output.Table(headers, rows)
				},
			})
		},
	}
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		claim       string
		roleID      string
		environment string
	}
	command := &cobra.Command{
		Use:          "create",
		Short:        "Create an OIDC role mapping",
		Example:      `  arcane admin oidc-mappings create --claim docker-admins --role role_admin`,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.OidcRoleMapping]{
				Method:         http.MethodPost,
				Path:           clitypes.OidcRoleMappings(),
				FailureMessage: "failed to create OIDC mapping",
				Body: types.OidcRoleMapping{
					ClaimValue:    opts.claim,
					RoleID:        opts.roleID,
					EnvironmentID: kit.Ternary(cmd.Flags().Changed("environment") && opts.environment != "", new(opts.environment), nil),
				},
				Render: func(data types.OidcRoleMapping) error {
					output.Success("OIDC mapping created")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Claim value", data.ClaimValue)
					output.KeyValue("Role", data.RoleID)
					scope := "global"
					if data.EnvironmentID != nil {
						scope = *data.EnvironmentID
					}
					output.KeyValue("Scope", scope)
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.claim, "claim", "", "OIDC claim value (required)")
	command.Flags().StringVar(&opts.roleID, "role", "", "Role ID to grant when the claim matches (required)")
	command.Flags().StringVar(&opts.environment, "environment", "", "Scope the grant to one environment (omit for global)")
	_ = command.MarkFlagRequired("claim")
	_ = command.MarkFlagRequired("role")
	return command
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		claim       string
		roleID      string
		environment string
	}
	command := &cobra.Command{
		Use:          "update <mapping-id>",
		Short:        "Update an OIDC role mapping",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			body := map[string]any{}
			for _, field := range []struct {
				flag  string
				key   string
				value any
			}{
				{flag: "claim", key: "claimValue", value: opts.claim},
				{flag: "role", key: "roleId", value: opts.roleID},
				{flag: "environment", key: "environmentId", value: kit.Ternary[any](opts.environment == "", nil, opts.environment)},
			} {
				if cmd.Flags().Changed(field.flag) {
					body[field.key] = field.value
				}
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.OidcRoleMapping]{
				Method:         http.MethodPut,
				Path:           clitypes.OidcRoleMapping(args[0]),
				Body:           body,
				FailureMessage: "failed to update OIDC mapping",
				Render: func(types.OidcRoleMapping) error {
					output.Success("OIDC mapping updated")
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.claim, "claim", "", "New claim value")
	command.Flags().StringVar(&opts.roleID, "role", "", "New role ID")
	command.Flags().StringVar(&opts.environment, "environment", "", "New environment scope (pass empty to make global)")
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <mapping-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete an OIDC role mapping",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Delete OIDC mapping %s?", args[0]),
				Path:           clitypes.OidcRoleMapping(args[0]),
				FailureMessage: "failed to delete OIDC mapping",
				SuccessMessage: "OIDC mapping deleted",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}
