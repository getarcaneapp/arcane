package users

import (
	"cmp"
	"fmt"
	"net/http"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command is the parent command for user operations
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "users",
		Aliases: []string{"user", "usr"},
		Short:   "Manage users",
		Long:    "Manage users. Role assignments are managed separately via `arcane admin roles assign` — the legacy `--role` flag has been removed alongside the binary admin/user model.",
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
		Short:        "List users",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.User]{
				Resource: "users",
				Endpoint: clitypes.Users(),
				Params:   cmdutil.ListParams{Resource: "users", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "USERNAME", "DISPLAY NAME", "EMAIL", "ROLE ASSIGNMENTS"},
				Row: func(usr types.User) []string {
					return []string{
						usr.ID,
						usr.Username,
						kit.FromPtr(usr.DisplayName),
						kit.FromPtr(usr.Email),
						roleAssignmentSummary(usr.RoleAssignments),
					}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of users to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return command
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		username    string
		password    string
		displayName string
		email       string
	}
	command := &cobra.Command{
		Use:   "create",
		Short: "Create a new user",
		Long:  "Create a new user. The user is created without any role assignments — grant them via `arcane admin roles assign <userId>` after creation.",

		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			if opts.password == "" {
				if opts.password, err = output.Secret(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), "Password", "password"); err != nil {
					return fmt.Errorf("failed to read password: %w", err)
				}
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.User]{
				Method:         http.MethodPost,
				Path:           clitypes.Users(),
				FailureMessage: "failed to create user",
				Body: types.User{
					Username:    opts.username,
					Password:    opts.password,
					DisplayName: kit.Ternary(cmd.Flags().Changed("display-name"), &opts.displayName, nil),
					Email:       kit.Ternary(cmd.Flags().Changed("email"), &opts.email, nil),
				},
				Render: func(data types.User) error {
					output.Success("User %s created successfully", data.Username)
					output.KeyValue("ID", data.ID)
					output.KeyValue("Username", data.Username)
					output.Info("No roles assigned. Use `arcane admin roles assign %s --role <roleId>` to grant access.", data.ID)
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.username, "username", "", "Username")
	command.Flags().StringVar(&opts.password, "password", "", "Password (if omitted, will prompt securely)")
	command.Flags().StringVar(&opts.displayName, "display-name", "", "Display name")
	command.Flags().StringVar(&opts.email, "email", "", "Email address")
	_ = command.MarkFlagRequired("username")
	return command
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <user-id>",
		Short:        "Get user details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.User]{
				Method:         http.MethodGet,
				Path:           clitypes.User(args[0]),
				FailureMessage: "failed to get user",
				Render: func(data types.User) error {
					output.Header("User Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Username", data.Username)
					if data.DisplayName != nil {
						output.KeyValue("Display Name", *data.DisplayName)
					}
					if data.Email != nil {
						output.KeyValue("Email", *data.Email)
					}
					output.KeyValue("Created", data.CreatedAt)

					output.KeyValue("Role Assignments", roleAssignmentSummary(data.RoleAssignments))
					if len(data.RoleAssignments) > 0 {
						rows := make([][]string, len(data.RoleAssignments))
						for i, a := range data.RoleAssignments {
							env := "global"
							if a.EnvironmentID != nil {
								env = *a.EnvironmentID
							}
							rows[i] = []string{a.RoleID, env, a.Source}
						}
						if tableErr := output.Table([]string{"ROLE", "SCOPE", "SOURCE"}, rows); tableErr != nil {
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
		username    string
		displayName string
		email       string
	}
	command := &cobra.Command{
		Use:          "update <user-id>",
		Short:        "Update user profile (role assignments managed separately)",
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
				{flag: "username", key: "username", value: opts.username},
				{flag: "display-name", key: "displayName", value: opts.displayName},
				{flag: "email", key: "email", value: opts.email},
			} {
				if cmd.Flags().Changed(field.flag) {
					body[field.key] = field.value
				}
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.User]{
				Method:         http.MethodPatch,
				Path:           clitypes.User(args[0]),
				FailureMessage: "failed to update user",
				Body:           body,
				Render: func(types.User) error {
					output.Success("User updated successfully")
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.username, "username", "", "New username")
	command.Flags().StringVar(&opts.displayName, "display-name", "", "Display name")
	command.Flags().StringVar(&opts.email, "email", "", "Email address")
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <user-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete user",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete user %s?", args[0]),
				Path:           clitypes.User(args[0]),
				FailureMessage: "failed to delete user",
				SuccessMessage: "User deleted successfully",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}

func roleAssignmentSummary(assignments []types.RoleAssignmentSummary) string {
	type bucket struct {
		envScoped int
		hasGlobal bool
	}
	buckets := map[string]*bucket{}
	var order []string
	for _, a := range assignments {
		b, ok := buckets[a.RoleID]
		if !ok {
			b = &bucket{}
			buckets[a.RoleID] = b
			order = append(order, a.RoleID)
		}
		if a.EnvironmentID == nil {
			b.hasGlobal = true
		} else {
			b.envScoped++
		}
	}
	parts := make([]string, 0, len(order))
	for _, roleID := range order {
		b := buckets[roleID]
		switch {
		case b.hasGlobal && b.envScoped == 0:
			parts = append(parts, roleID+" (global)")
		case b.hasGlobal && b.envScoped > 0:
			parts = append(parts, fmt.Sprintf("%s (global +%d envs)", roleID, b.envScoped))
		case b.envScoped == 1:
			parts = append(parts, roleID+" on 1 env")
		default:
			parts = append(parts, fmt.Sprintf("%s on %d envs", roleID, b.envScoped))
		}
	}
	return cmp.Or(strings.Join(parts, ", "), "—")
}
