package groups

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "groups",
		Aliases: []string{"group", "grp"},
		Short:   "Manage user groups",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newGetCmd())
	command.AddCommand(newCreateCmd())
	command.AddCommand(newUpdateCmd())
	command.AddCommand(newDeleteCmd())
	command.AddCommand(newMembersCmd())
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
		Short:        "List groups",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.Group]{
				Resource: "groups",
				Endpoint: clitypes.Groups(),
				Params:   cmdutil.ListParams{Resource: "groups", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "NAME", "DESCRIPTION", "MEMBERS", "BINDINGS"},
				Row: func(g types.Group) []string {
					return []string{
						g.ID,
						g.Name,
						kit.FromPtr(g.Description),
						strconv.Itoa(g.MemberCount),
						strconv.Itoa(g.BindingCount),
					}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of groups to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return command
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <group-id>",
		Short:        "Get group details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Group]{
				Method:         http.MethodGet,
				Path:           clitypes.Group(args[0]),
				FailureMessage: "failed to get group",
				Render: func(data types.Group) error {
					output.Header("Group Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					if data.Description != nil {
						output.KeyValue("Description", *data.Description)
					}
					output.KeyValue("Members", strconv.Itoa(data.MemberCount))
					output.KeyValue("Bindings", strconv.Itoa(data.BindingCount))
					output.KeyValue("Created", data.CreatedAt.Format("2006-01-02 15:04"))
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
	}
	command := &cobra.Command{
		Use:          "create",
		Short:        "Create a group",
		Example:      `  arcane admin groups create --name operators -d "Day-to-day operators"`,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Group]{
				Method:         http.MethodPost,
				Path:           clitypes.Groups(),
				FailureMessage: "failed to create group",
				Body: types.CreateGroup{
					Name:        opts.name,
					Description: kit.Ternary(cmd.Flags().Changed("description"), &opts.description, nil),
				},
				Render: func(data types.Group) error {
					output.Success("Group %s created", data.Name)
					output.KeyValue("ID", data.ID)
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.name, "name", "", "Group name (required)")
	command.Flags().StringVarP(&opts.description, "description", "d", "", "Group description")
	_ = command.MarkFlagRequired("name")
	return command
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		name        string
		description string
	}
	command := &cobra.Command{
		Use:          "update <group-id>",
		Short:        "Update a group",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Group]{
				Method:         http.MethodPatch,
				Path:           clitypes.Group(args[0]),
				FailureMessage: "failed to update group",
				Body: types.UpdateGroup{
					Name:        kit.Ternary(cmd.Flags().Changed("name"), &opts.name, nil),
					Description: kit.Ternary(cmd.Flags().Changed("description"), &opts.description, nil),
				},
				Render: func(types.Group) error {
					output.Success("Group updated")
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.name, "name", "", "New group name")
	command.Flags().StringVarP(&opts.description, "description", "d", "", "New group description")
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <group-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a group",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Delete group %s? This removes its memberships and bindings.", args[0]),
				Path:           clitypes.Group(args[0]),
				FailureMessage: "failed to delete group",
				SuccessMessage: "Group deleted",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}

func newMembersCmd() *cobra.Command {
	command := &cobra.Command{
		Use:     "members",
		Aliases: []string{"member"},
		Short:   "Manage group members",
	}
	command.AddCommand(newMembersListCmd())
	command.AddCommand(newMembersAddCmd())
	command.AddCommand(newMembersRemoveCmd())
	return command
}

func renderMembers(data []types.GroupMember) error {
	if len(data) == 0 {
		output.Info("No members")
		return nil
	}
	rows := make([][]string, len(data))
	for i, m := range data {
		rows[i] = []string{
			m.UserID,
			m.Username,
			kit.FromPtr(m.DisplayName),
			m.Source,
			m.CreatedAt.Format("2006-01-02 15:04"),
		}
	}
	return output.Table([]string{"USER ID", "USERNAME", "DISPLAY NAME", "SOURCE", "ADDED"}, rows)
}

func newMembersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list <group-id>",
		Aliases:      []string{"ls"},
		Short:        "List group members",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.GroupMember]{
				Method:         http.MethodGet,
				Path:           clitypes.GroupMembers(args[0]),
				FailureMessage: "failed to list group members",
				Render:         renderMembers,
			})
		},
	}
}

func newMembersAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "add <group-id> <user-id>...",
		Short:        "Add users to a group",
		Args:         cobra.MinimumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.GroupMember]{
				Method:         http.MethodPost,
				Path:           clitypes.GroupMembers(args[0]),
				Body:           types.AddGroupMembers{UserIDs: args[1:]},
				FailureMessage: "failed to add group members",
				Render: func(data []types.GroupMember) error {
					output.Success("Added %d member(s)", len(args)-1)
					return renderMembers(data)
				},
			})
		},
	}
}

func newMembersRemoveCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "remove <group-id> <user-id>",
		Aliases:      []string{"rm"},
		Short:        "Remove a user from a group",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Remove user %s from group %s?", args[1], args[0]),
				Path:           clitypes.GroupMember(args[0], args[1]),
				FailureMessage: "failed to remove group member",
				SuccessMessage: "Group member removed",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Remove without confirmation")
	return command
}
