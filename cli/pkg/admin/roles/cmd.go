// Package roles manages roles and user assignments.
package roles

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command is the parent command for role operations.
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "roles",
		Aliases: []string{"role"},
		Short:   "Manage roles and per-user role assignments",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newGetCmd())
	command.AddCommand(newCreateCmd())
	command.AddCommand(newUpdateCmd())
	command.AddCommand(newDeleteCmd())
	command.AddCommand(newPermissionsCmd())
	command.AddCommand(newAssignmentsCmd())
	command.AddCommand(newAssignCmd())
	return command
}

// ---------- Role CRUD ----------

func newListCmd() *cobra.Command {
	var opts struct {
		limit int
		start int
		all   bool
	}
	command := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List roles",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.Role]{
				Resource: "roles",
				Endpoint: clitypes.Roles(),
				Params:   cmdutil.ListParams{Resource: "roles", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "NAME", "TYPE", "USERS", "PERMISSIONS"},
				Row: func(r types.Role) []string {
					return []string{
						r.ID,
						r.Name,
						kit.Ternary(r.BuiltIn, "built-in", "custom"),
						strconv.Itoa(r.AssignedUserCount),
						strconv.Itoa(len(r.Permissions)),
					}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of roles to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return command
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <role-id>",
		Short:        "Get role details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Role]{
				Method:         http.MethodGet,
				Path:           clitypes.Role(args[0]),
				FailureMessage: "failed to get role",
				Render: func(data types.Role) error {
					output.Header("Role Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					if data.Description != nil {
						output.KeyValue("Description", *data.Description)
					}
					output.KeyValue("Built-in", strconv.FormatBool(data.BuiltIn))
					output.KeyValue("Assigned users", strconv.Itoa(data.AssignedUserCount))
					output.KeyValue("Permissions", strconv.Itoa(len(data.Permissions)))
					if len(data.Permissions) > 0 {
						rows := make([][]string, len(data.Permissions))
						for i, p := range data.Permissions {
							rows[i] = []string{p}
						}
						if tableErr := output.Table([]string{"PERMISSION"}, rows); tableErr != nil {
							return tableErr
						}
					}
					return nil
				},
			})
		},
	}
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		name        string
		description string
		permissions []string
	}
	command := &cobra.Command{
		Use:          "create",
		Short:        "Create a custom role",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.name == "" {
				return errors.New("--name is required")
			}
			if len(opts.permissions) == 0 {
				return errors.New("at least one --permission is required")
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Role]{
				Method:         http.MethodPost,
				Path:           clitypes.Roles(),
				FailureMessage: "failed to create role",
				Body: types.Role{
					Name:        opts.name,
					Permissions: opts.permissions,
					Description: kit.Ternary(cmd.Flags().Changed("description"), &opts.description, nil),
				},
				Render: func(data types.Role) error {
					output.Success("Role %s created", data.Name)
					output.KeyValue("ID", data.ID)
					output.KeyValue("Permissions", strconv.Itoa(len(data.Permissions)))
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.name, "name", "", "Role name (required)")
	command.Flags().StringVarP(&opts.description, "description", "d", "", "Role description")
	command.Flags().StringArrayVarP(&opts.permissions, "permission", "p", nil, "Permission to grant (repeatable, e.g. containers:start)")
	return command
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		name        string
		description string
		permissions []string
	}
	command := &cobra.Command{
		Use:          "update <role-id>",
		Short:        "Update a custom role (built-in roles are immutable)",
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
				{flag: "name", key: "name", value: opts.name},
				{flag: "description", key: "description", value: opts.description},
				{flag: "permission", key: "permissions", value: opts.permissions},
			} {
				if cmd.Flags().Changed(field.flag) {
					body[field.key] = field.value
				}
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Role]{
				Method:         http.MethodPut,
				Path:           clitypes.Role(args[0]),
				Body:           body,
				FailureMessage: "failed to update role",
				Render: func(types.Role) error {
					output.Success("Role updated")
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.name, "name", "", "New role name")
	command.Flags().StringVarP(&opts.description, "description", "d", "", "New role description")
	command.Flags().StringArrayVarP(&opts.permissions, "permission", "p", nil, "Replace the permission set (repeatable)")
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <role-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a custom role (built-in roles cannot be deleted)",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Delete role %s? This removes every assignment of it.", args[0]),
				Path:           clitypes.Role(args[0]),
				FailureMessage: "failed to delete role",
				SuccessMessage: "Role deleted",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}

// ---------- Permission manifest ----------

func newPermissionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "permissions",
		Aliases:      []string{"perms"},
		Short:        "List every permission the server recognizes (the manifest)",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.PermissionsManifest]{
				Method:         http.MethodGet,
				Path:           clitypes.RolesAvailablePermissions(),
				FailureMessage: "failed to load permission manifest",
				Render: func(data types.PermissionsManifest) error {
					for _, group := range data.Resources {
						output.Header("%s (%s)", group.Label, group.Scope)
						rows := make([][]string, len(group.Actions))
						for i, a := range group.Actions {
							rows[i] = []string{a.Permission, a.Label, a.Description}
						}
						if tableErr := output.Table([]string{"PERMISSION", "LABEL", "DESCRIPTION"}, rows); tableErr != nil {
							return tableErr
						}
					}
					return nil
				},
			})
		},
	}
}

// ---------- User assignments ----------

func newAssignmentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "assignments <user-id>",
		Short:        "List a user's role assignments",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.RoleAssignment]{
				Method:         http.MethodGet,
				Path:           clitypes.UserRoleAssignments(args[0]),
				FailureMessage: "failed to list assignments",
				Render: func(data []types.RoleAssignment) error {
					if len(data) == 0 {
						output.Info("No role assignments for user %s", args[0])
						return nil
					}
					rows := make([][]string, len(data))
					for i, a := range data {
						scope := "global"
						if a.EnvironmentID != nil {
							scope = *a.EnvironmentID
						}
						rows[i] = []string{a.RoleID, scope, a.Source, a.CreatedAt.Format("2006-01-02 15:04")}
					}
					return output.Table([]string{"ROLE", "SCOPE", "SOURCE", "CREATED"}, rows)
				},
			})
		},
	}
}

func newAssignCmd() *cobra.Command {
	var opts struct {
		roles []string
	}
	command := &cobra.Command{
		Use:   "assign <user-id>",
		Short: "Replace a user's manual role assignments",
		Long: "Replace every MANUAL role assignment on the user with the set passed via --role. OIDC-sourced assignments are " +
			"left untouched — manage those via OIDC role mappings.\n\nEach --role flag accepts `<roleId>[:<envId>]`. Omit " +
			"the env id for a global assignment. Pass --role multiple times to assign more than one role. Pass --role \"\" " +
			"(empty) once to clear every manual assignment.",

		Example: `  arcane admin roles assign u_123 --role role_editor:env_prod --role role_viewer
  arcane admin roles assign u_123 --role role_admin
  arcane admin roles assign u_123 --role ""    # clear all manual assignments`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			assignments := make([]types.UserRoleAssignmentInput, 0, len(opts.roles))
			for _, raw := range opts.roles {
				token := strings.TrimSpace(raw)
				if token == "" {
					continue
				}
				roleID, envID, hasEnv := strings.Cut(token, ":")
				assignments = append(assignments, types.UserRoleAssignmentInput{
					RoleID:        roleID,
					EnvironmentID: kit.Ternary(hasEnv && envID != "", new(envID), nil),
				})
			}
			req := types.SetUserRoleAssignments{
				Assignments: assignments,
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.RoleAssignment]{
				Method:         http.MethodPut,
				Path:           clitypes.UserRoleAssignments(args[0]),
				FailureMessage: "failed to set assignments",
				Body:           req,
				Render: func(data []types.RoleAssignment) error {
					output.Success("User %s now has %d role assignment(s)", args[0], len(data))
					return nil
				},
			})
		},
	}
	command.Flags().StringArrayVar(&opts.roles, "role", nil, "Role to grant as `<roleId>[:<envId>]` (repeatable). Pass --role \"\" to clear all manual assignments.")
	_ = command.MarkFlagRequired("role")
	return command
}
