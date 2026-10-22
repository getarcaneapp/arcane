// Package oidcmappings manages OIDC claim-to-group mappings.
package oidcmappings

import (
	"fmt"
	"net/http"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the parent command for OIDC group mapping operations.
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "oidc",
		Aliases: []string{"oidc-mappings", "oidc-mapping"},
		Short:   "Manage OIDC claim → group mappings",
		Long: "Manage OIDC claim → group mappings. On every OIDC login, Arcane looks up the user's groups claim and applies " +
			"the matching mappings as source='oidc' group memberships. The groups claim itself is configured via the " +
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
		Short:        "List OIDC group mappings",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.OidcGroupMapping]{
				Method:         http.MethodGet,
				Path:           clitypes.OidcMappings(),
				FailureMessage: "failed to list OIDC mappings",
				Render: func(data []types.OidcGroupMapping) error {
					if len(data) == 0 {
						output.Info("No OIDC group mappings configured")
						return nil
					}
					rows := make([][]string, len(data))
					for i, m := range data {
						rows[i] = []string{m.ID, m.ClaimValue, m.GroupName, m.Source}
					}
					return output.Table([]string{"ID", "CLAIM VALUE", "GROUP", "SOURCE"}, rows)
				},
			})
		},
	}
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		claim   string
		groupID string
	}
	command := &cobra.Command{
		Use:          "create",
		Short:        "Create an OIDC group mapping",
		Example:      `  arcane admin oidc-mappings create --claim docker-admins --group <group-id>`,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.OidcGroupMapping]{
				Method:         http.MethodPost,
				Path:           clitypes.OidcMappings(),
				FailureMessage: "failed to create OIDC mapping",
				Body: types.OidcGroupMappingSpec{
					ClaimValue: opts.claim,
					GroupID:    opts.groupID,
				},
				Render: func(data types.OidcGroupMapping) error {
					output.Success("OIDC mapping created")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Claim value", data.ClaimValue)
					output.KeyValue("Group", data.GroupName)
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.claim, "claim", "", "OIDC claim value (required)")
	command.Flags().StringVar(&opts.groupID, "group", "", "Group ID to grant membership in when the claim matches (required)")
	_ = command.MarkFlagRequired("claim")
	_ = command.MarkFlagRequired("group")
	return command
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		claim   string
		groupID string
	}
	command := &cobra.Command{
		Use:          "update <mapping-id>",
		Short:        "Update an OIDC group mapping",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.OidcGroupMapping]{
				Method: http.MethodPatch,
				Path:   clitypes.OidcMapping(args[0]),
				Body: types.OidcGroupMappingSpec{
					ClaimValue: opts.claim,
					GroupID:    opts.groupID,
				},
				FailureMessage: "failed to update OIDC mapping",
				Render: func(types.OidcGroupMapping) error {
					output.Success("OIDC mapping updated")
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.claim, "claim", "", "Claim value (required)")
	command.Flags().StringVar(&opts.groupID, "group", "", "Group ID (required)")
	_ = command.MarkFlagRequired("claim")
	_ = command.MarkFlagRequired("group")
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <mapping-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete an OIDC group mapping",
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
				Path:           clitypes.OidcMapping(args[0]),
				FailureMessage: "failed to delete OIDC mapping",
				SuccessMessage: "OIDC mapping deleted",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}
