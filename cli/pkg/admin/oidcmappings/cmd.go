// Package oidcmappings provides the `arcane admin oidc-mappings` command tree.
// Mappings convert OIDC group/claim values into role assignments on every
// login — they're the SSO-driven counterpart to manual role assignments.
package oidcmappings

import (
	"fmt"

	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	roletypes "github.com/getarcaneapp/arcane/types/role"
	"github.com/spf13/cobra"
)

var (
	forceFlag  bool
	jsonOutput bool
)

var (
	createClaim       string
	createRoleID      string
	createEnvironment string
)

var (
	updateClaim       string
	updateRoleID      string
	updateEnvironment string
)

// OidcMappingsCmd is the parent command for OIDC role mapping operations.
var OidcMappingsCmd = &cobra.Command{
	Use:     "oidc-mappings",
	Aliases: []string{"oidc-mapping", "oidc"},
	Short:   "Manage OIDC group → role mappings",
	Long: "Manage OIDC group → role mappings. On every OIDC login, Arcane " +
		"looks up the user's groups claim and applies the matching mappings " +
		"as source='oidc' role assignments. The groups claim itself is " +
		"configured via the oidcGroupsClaim setting (default `groups`).",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List OIDC role mappings",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		resp, err := c.Get(cmd.Context(), types.Endpoints.OidcRoleMappings())
		if err != nil {
			return fmt.Errorf("failed to list OIDC mappings: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]roletypes.OidcRoleMapping]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to list OIDC mappings: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		if len(result.Data) == 0 {
			output.Info("No OIDC role mappings configured")
			return nil
		}

		headers := []string{"ID", "CLAIM VALUE", "ROLE", "SCOPE"}
		rows := make([][]string, len(result.Data))
		for i, m := range result.Data {
			scope := "global"
			if m.EnvironmentID != nil {
				scope = *m.EnvironmentID
			}
			rows[i] = []string{m.ID, m.ClaimValue, m.RoleID, scope}
		}
		output.Table(headers, rows)
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create an OIDC role mapping",
	Example:      `  arcane admin oidc-mappings create --claim docker-admins --role role_admin`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		req := roletypes.CreateOidcRoleMapping{
			ClaimValue: createClaim,
			RoleID:     createRoleID,
		}
		if cmd.Flags().Changed("environment") && createEnvironment != "" {
			req.EnvironmentID = new(createEnvironment)
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.OidcRoleMappings(), req)
		if err != nil {
			return fmt.Errorf("failed to create OIDC mapping: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var result base.ApiResponse[roletypes.OidcRoleMapping]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to create OIDC mapping: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}
		output.Success("OIDC mapping created")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Claim value", result.Data.ClaimValue)
		output.KeyValue("Role", result.Data.RoleID)
		scope := "global"
		if result.Data.EnvironmentID != nil {
			scope = *result.Data.EnvironmentID
		}
		output.KeyValue("Scope", scope)
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <mapping-id>",
	Short:        "Update an OIDC role mapping",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		// The PUT contract requires every field — fetch current and overlay.
		current, err := fetchMappingInternal(cmd, args[0])
		if err != nil {
			return err
		}

		req := roletypes.UpdateOidcRoleMapping{
			ClaimValue:    current.ClaimValue,
			RoleID:        current.RoleID,
			EnvironmentID: current.EnvironmentID,
		}
		if cmd.Flags().Changed("claim") {
			req.ClaimValue = updateClaim
		}
		if cmd.Flags().Changed("role") {
			req.RoleID = updateRoleID
		}
		if cmd.Flags().Changed("environment") {
			if updateEnvironment == "" {
				// Empty value flips an env-scoped mapping back to global.
				req.EnvironmentID = nil
			} else {
				req.EnvironmentID = new(updateEnvironment)
			}
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.OidcRoleMapping(args[0]), req)
		if err != nil {
			return fmt.Errorf("failed to update OIDC mapping: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to update OIDC mapping: %w", err)
		}
		output.Success("OIDC mapping updated")
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <mapping-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete an OIDC role mapping",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Delete OIDC mapping %s?", args[0]))
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
		resp, err := c.Delete(cmd.Context(), types.Endpoints.OidcRoleMapping(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete OIDC mapping: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete OIDC mapping: %w", err)
		}
		output.Success("OIDC mapping deleted")
		return nil
	},
}

func fetchMappingInternal(cmd *cobra.Command, id string) (*roletypes.OidcRoleMapping, error) {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return nil, err
	}
	resp, err := c.Get(cmd.Context(), types.Endpoints.OidcRoleMapping(id))
	if err != nil {
		return nil, fmt.Errorf("failed to load current mapping: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var result base.ApiResponse[roletypes.OidcRoleMapping]
	if err := cmdutil.DecodeJSON(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to load current mapping: %w", err)
	}
	return &result.Data, nil
}

func init() {
	OidcMappingsCmd.AddCommand(listCmd)
	OidcMappingsCmd.AddCommand(createCmd)
	OidcMappingsCmd.AddCommand(updateCmd)
	OidcMappingsCmd.AddCommand(deleteCmd)

	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	createCmd.Flags().StringVar(&createClaim, "claim", "", "OIDC claim value (required)")
	createCmd.Flags().StringVar(&createRoleID, "role", "", "Role ID to grant when the claim matches (required)")
	createCmd.Flags().StringVar(&createEnvironment, "environment", "", "Scope the grant to one environment (omit for global)")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = createCmd.MarkFlagRequired("claim")
	_ = createCmd.MarkFlagRequired("role")

	updateCmd.Flags().StringVar(&updateClaim, "claim", "", "New claim value")
	updateCmd.Flags().StringVar(&updateRoleID, "role", "", "New role ID")
	updateCmd.Flags().StringVar(&updateEnvironment, "environment", "", "New environment scope (pass empty to make global)")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
}
