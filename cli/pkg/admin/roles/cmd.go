// Package roles provides the `arcane admin roles` command tree for RBAC.
// Roles are named permission sets that can be granted to users (globally or
// per-environment) and to API keys. Four built-in roles (Admin, Editor,
// Deployer, Viewer) ship with Arcane and cannot be modified; everything else
// is a custom role created via this command.
package roles

import (
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	roletypes "github.com/getarcaneapp/arcane/types/role"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	startFlag  int
	forceFlag  bool
	jsonOutput bool
)

var (
	roleCreateName        string
	roleCreateDescription string
	roleCreatePermissions []string
)

var (
	roleUpdateName        string
	roleUpdateDescription string
	roleUpdatePermissions []string
)

// `assign` flags. Each --role argument is parsed as `<roleId>[:<envId>]`.
// Omitting the env id gives a global assignment.
var (
	assignRoles []string
)

// RolesCmd is the parent command for role operations.
var RolesCmd = &cobra.Command{
	Use:     "roles",
	Aliases: []string{"role"},
	Short:   "Manage roles and per-user role assignments",
}

// ---------- Role CRUD ----------

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List roles",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path := types.Endpoints.Roles()
		path, err = cmdutil.ApplyPaginationParams(cmd, path, "roles", "limit", limitFlag, 20, "start", startFlag)
		if err != nil {
			return fmt.Errorf("failed to build pagination query: %w", err)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list roles: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[roletypes.Role]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to list roles: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result)
		}

		headers := []string{"ID", "NAME", "TYPE", "USERS", "PERMISSIONS"}
		rows := make([][]string, len(result.Data))
		for i, r := range result.Data {
			roleType := "custom"
			if r.BuiltIn {
				roleType = "built-in"
			}
			rows[i] = []string{
				r.ID,
				r.Name,
				roleType,
				fmt.Sprintf("%d", r.AssignedUserCount),
				fmt.Sprintf("%d", len(r.Permissions)),
			}
		}
		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "roles")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <role-id>",
	Short:        "Get role details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		resp, err := c.Get(cmd.Context(), types.Endpoints.Role(args[0]))
		if err != nil {
			return fmt.Errorf("failed to get role: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[roletypes.Role]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to get role: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Role Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		if result.Data.Description != nil {
			output.KeyValue("Description", *result.Data.Description)
		}
		output.KeyValue("Built-in", fmt.Sprintf("%t", result.Data.BuiltIn))
		output.KeyValue("Assigned users", fmt.Sprintf("%d", result.Data.AssignedUserCount))
		output.KeyValue("Permissions", fmt.Sprintf("%d", len(result.Data.Permissions)))
		if len(result.Data.Permissions) > 0 {
			rows := make([][]string, len(result.Data.Permissions))
			for i, p := range result.Data.Permissions {
				rows[i] = []string{p}
			}
			output.Table([]string{"PERMISSION"}, rows)
		}
		return nil
	},
}

var createCmd = &cobra.Command{
	Use:          "create",
	Short:        "Create a custom role",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if roleCreateName == "" {
			return fmt.Errorf("--name is required")
		}
		if len(roleCreatePermissions) == 0 {
			return fmt.Errorf("at least one --permission is required")
		}
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		req := roletypes.CreateRole{
			Name:        roleCreateName,
			Permissions: roleCreatePermissions,
		}
		if cmd.Flags().Changed("description") {
			req.Description = &roleCreateDescription
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.Roles(), req)
		if err != nil {
			return fmt.Errorf("failed to create role: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var result base.ApiResponse[roletypes.Role]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to create role: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}
		output.Success("Role %s created", result.Data.Name)
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Permissions", fmt.Sprintf("%d", len(result.Data.Permissions)))
		return nil
	},
}

var updateCmd = &cobra.Command{
	Use:          "update <role-id>",
	Short:        "Update a custom role (built-in roles are immutable)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		// The PUT contract requires every field — fetch the current state
		// and overlay only the flags the caller set, so partial updates work
		// from the CLI without forcing the user to retype everything.
		current, err := fetchRoleInternal(cmd, args[0])
		if err != nil {
			return err
		}

		req := roletypes.UpdateRole{
			Name:        current.Name,
			Description: current.Description,
			Permissions: current.Permissions,
		}
		if cmd.Flags().Changed("name") {
			req.Name = roleUpdateName
		}
		if cmd.Flags().Changed("description") {
			req.Description = &roleUpdateDescription
		}
		if cmd.Flags().Changed("permission") {
			req.Permissions = roleUpdatePermissions
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.Role(args[0]), req)
		if err != nil {
			return fmt.Errorf("failed to update role: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to update role: %w", err)
		}

		output.Success("Role updated")
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <role-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a custom role (built-in roles cannot be deleted)",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Delete role %s? This removes every assignment of it.", args[0]))
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
		resp, err := c.Delete(cmd.Context(), types.Endpoints.Role(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete role: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to delete role: %w", err)
		}
		output.Success("Role deleted")
		return nil
	},
}

// ---------- Permission manifest ----------

var permissionsCmd = &cobra.Command{
	Use:          "permissions",
	Aliases:      []string{"perms"},
	Short:        "List every permission the server recognizes (the manifest)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		resp, err := c.Get(cmd.Context(), types.Endpoints.RolesAvailablePermissions())
		if err != nil {
			return fmt.Errorf("failed to load permission manifest: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[roletypes.PermissionsManifest]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to load permission manifest: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		for _, group := range result.Data.Resources {
			output.Header("%s (%s)", group.Label, group.Scope)
			rows := make([][]string, len(group.Actions))
			for i, a := range group.Actions {
				rows[i] = []string{a.Permission, a.Label, a.Description}
			}
			output.Table([]string{"PERMISSION", "LABEL", "DESCRIPTION"}, rows)
		}
		return nil
	},
}

// ---------- User assignments ----------

var assignmentsCmd = &cobra.Command{
	Use:          "assignments <user-id>",
	Short:        "List a user's role assignments",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		resp, err := c.Get(cmd.Context(), types.Endpoints.UserRoleAssignments(args[0]))
		if err != nil {
			return fmt.Errorf("failed to list assignments: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]roletypes.RoleAssignment]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to list assignments: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		if len(result.Data) == 0 {
			output.Info("No role assignments for user %s", args[0])
			return nil
		}
		rows := make([][]string, len(result.Data))
		for i, a := range result.Data {
			scope := "global"
			if a.EnvironmentID != nil {
				scope = *a.EnvironmentID
			}
			rows[i] = []string{a.RoleID, scope, a.Source, a.CreatedAt.Format("2006-01-02 15:04")}
		}
		output.Table([]string{"ROLE", "SCOPE", "SOURCE", "CREATED"}, rows)
		return nil
	},
}

var assignCmd = &cobra.Command{
	Use:   "assign <user-id>",
	Short: "Replace a user's manual role assignments",
	Long: "Replace every MANUAL role assignment on the user with the set " +
		"passed via --role. OIDC-sourced assignments are left untouched — " +
		"manage those via OIDC role mappings.\n\n" +
		"Each --role flag accepts `<roleId>[:<envId>]`. Omit the env id for " +
		"a global assignment. Pass --role multiple times to assign more than " +
		"one role. Pass --role \"\" (empty) once to clear every manual " +
		"assignment.",
	Example: `  arcane admin roles assign u_123 --role role_editor:env_prod --role role_viewer
  arcane admin roles assign u_123 --role role_admin
  arcane admin roles assign u_123 --role ""    # clear all manual assignments`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		req := roletypes.SetUserAssignments{
			Assignments: parseAssignmentsInternal(assignRoles),
		}
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}
		resp, err := c.Put(cmd.Context(), types.Endpoints.UserRoleAssignments(args[0]), req)
		if err != nil {
			return fmt.Errorf("failed to set assignments: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		var result base.ApiResponse[[]roletypes.RoleAssignment]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to set assignments: %w", err)
		}
		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}
		output.Success("User %s now has %d role assignment(s)", args[0], len(result.Data))
		return nil
	},
}

// ---------- helpers ----------

// parseAssignmentsInternal converts CLI --role tokens into the request payload.
// Format: "<roleId>" (global) or "<roleId>:<envId>" (env-scoped). A single
// empty token means "clear all assignments".
func parseAssignmentsInternal(tokens []string) []roletypes.UserAssignmentInput {
	if len(tokens) == 1 && strings.TrimSpace(tokens[0]) == "" {
		return []roletypes.UserAssignmentInput{}
	}
	out := make([]roletypes.UserAssignmentInput, 0, len(tokens))
	for _, raw := range tokens {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		roleID, envID, hasEnv := strings.Cut(token, ":")
		entry := roletypes.UserAssignmentInput{RoleID: roleID}
		if hasEnv && envID != "" {
			entry.EnvironmentID = new(envID)
		}
		out = append(out, entry)
	}
	return out
}

func fetchRoleInternal(cmd *cobra.Command, id string) (*roletypes.Role, error) {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return nil, err
	}
	resp, err := c.Get(cmd.Context(), types.Endpoints.Role(id))
	if err != nil {
		return nil, fmt.Errorf("failed to load current role: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var result base.ApiResponse[roletypes.Role]
	if err := cmdutil.DecodeJSON(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to load current role: %w", err)
	}
	return &result.Data, nil
}

func init() {
	RolesCmd.AddCommand(listCmd)
	RolesCmd.AddCommand(getCmd)
	RolesCmd.AddCommand(createCmd)
	RolesCmd.AddCommand(updateCmd)
	RolesCmd.AddCommand(deleteCmd)
	RolesCmd.AddCommand(permissionsCmd)
	RolesCmd.AddCommand(assignmentsCmd)
	RolesCmd.AddCommand(assignCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of roles to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, "Offset for pagination")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	createCmd.Flags().StringVar(&roleCreateName, "name", "", "Role name (required)")
	createCmd.Flags().StringVarP(&roleCreateDescription, "description", "d", "", "Role description")
	createCmd.Flags().StringArrayVarP(&roleCreatePermissions, "permission", "p", nil, "Permission to grant (repeatable, e.g. containers:start)")
	createCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	updateCmd.Flags().StringVar(&roleUpdateName, "name", "", "New role name")
	updateCmd.Flags().StringVarP(&roleUpdateDescription, "description", "d", "", "New role description")
	updateCmd.Flags().StringArrayVarP(&roleUpdatePermissions, "permission", "p", nil, "Replace the permission set (repeatable)")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")

	permissionsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	assignmentsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	assignCmd.Flags().StringArrayVar(&assignRoles, "role", nil, "Role to grant as `<roleId>[:<envId>]` (repeatable). Pass --role \"\" to clear all manual assignments.")
	assignCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = assignCmd.MarkFlagRequired("role")
}
