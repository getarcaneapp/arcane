package bindings

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "bindings",
		Aliases: []string{"binding", "rb"},
		Short:   "Manage role bindings",
		Long: "Manage role bindings. A binding grants a role to a user or group, globally or in one environment, " +
			"optionally restricted by a resource selector (labels, projects, owner, name globs).",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newCreateCmd())
	command.AddCommand(newDeleteCmd())
	command.AddCommand(newAccessCmd())
	return command
}

type listOptions struct {
	limit       int
	start       int
	all         bool
	user        string
	group       string
	role        string
	environment string
}

func newListCmd() *cobra.Command {
	var opts listOptions
	command := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List role bindings",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, opts)
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of bindings to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	command.Flags().StringVar(&opts.user, "user", "", "Only bindings of this user ID")
	command.Flags().StringVar(&opts.group, "group", "", "Only bindings of this group ID")
	command.Flags().StringVar(&opts.role, "role", "", "Only bindings of this role ID")
	command.Flags().StringVar(&opts.environment, "environment", "", "Only bindings scoped to this environment ID")
	command.MarkFlagsMutuallyExclusive("user", "group")
	return command
}

func runList(cmd *cobra.Command, opts listOptions) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}
	query := url.Values{}
	if subjectID := cmp.Or(opts.user, opts.group); subjectID != "" {
		query.Set("subjectType", kit.Ternary(opts.user != "", "user", "group"))
		query.Set("subjectId", subjectID)
	}
	for key, value := range map[string]string{"roleId": opts.role, "environmentId": opts.environment} {
		if value != "" {
			query.Set(key, value)
		}
	}
	return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.RoleBinding]{
		Resource: "bindings",
		Endpoint: clitypes.RoleBindings(),
		Params:   cmdutil.ListParams{Resource: "bindings", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
		Query:    query,
		Headers:  []string{"ID", "SUBJECT", "ROLE", "SCOPE", "SELECTOR", "SOURCE"},
		Row: func(b types.RoleBinding) []string {
			return []string{
				b.ID,
				b.SubjectType + ":" + b.SubjectName,
				b.RoleName,
				kit.Ternary(b.EnvironmentID == nil, "global", cmp.Or(b.EnvironmentName, kit.FromPtr(b.EnvironmentID))),
				cmdutil.FormatSelector(kit.FromPtr(b.Selector)),
				b.Source,
			}
		},
	})
}

type createOptions struct {
	user        string
	group       string
	role        string
	environment string
	labels      map[string]string
	projects    []string
	ownerOnly   bool
	names       []string
}

func newCreateCmd() *cobra.Command {
	var opts createOptions
	command := &cobra.Command{
		Use:   "create",
		Short: "Bind a role to a user or group",
		Example: `  arcane admin bindings create --group <group-id> --role <role-id>
  arcane admin bindings create --user <user-id> --role <role-id> --environment 0 --label team=web --owner-only`,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd, opts)
		},
	}
	command.Flags().StringVar(&opts.user, "user", "", "User ID to bind")
	command.Flags().StringVar(&opts.group, "group", "", "Group ID to bind")
	command.Flags().StringVar(&opts.role, "role", "", "Role ID to bind (required)")
	command.Flags().StringVar(&opts.environment, "environment", "", "Environment ID to scope the binding to (omit for global)")
	command.Flags().StringToStringVar(&opts.labels, "label", nil, "Restrict to resources carrying this label (KEY=VALUE, repeatable)")
	command.Flags().StringArrayVar(&opts.projects, "project", nil, "Restrict to resources in this compose project (repeatable)")
	command.Flags().BoolVar(&opts.ownerOnly, "owner-only", false, "Restrict to resources owned by the bound subject")
	command.Flags().StringArrayVar(&opts.names, "name", nil, "Restrict to resource names matching this glob (repeatable)")
	command.MarkFlagsMutuallyExclusive("user", "group")
	command.MarkFlagsOneRequired("user", "group")
	_ = command.MarkFlagRequired("role")
	return command
}

func runCreate(cmd *cobra.Command, opts createOptions) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}
	selector := types.ResourceSelector{
		Labels:    opts.labels,
		Projects:  opts.projects,
		OwnerOnly: opts.ownerOnly,
		Names:     opts.names,
	}
	return cmdutil.Run(cmd, c, cmdutil.Spec[types.CreateRoleBindingResult]{
		Method:         http.MethodPost,
		Path:           clitypes.RoleBindings(),
		FailureMessage: "failed to create role binding",
		Body: types.CreateRoleBinding{
			SubjectType:   kit.Ternary(opts.user != "", "user", "group"),
			SubjectID:     cmp.Or(opts.user, opts.group),
			RoleID:        opts.role,
			EnvironmentID: kit.Ternary(opts.environment != "", &opts.environment, nil),
			Selector:      kit.Ternary(cmdutil.FormatSelector(selector) != "", &selector, nil),
		},
		Render: func(data types.CreateRoleBindingResult) error {
			output.Success("Bound role %s to %s %s", data.Binding.RoleName, data.Binding.SubjectType, data.Binding.SubjectName)
			output.KeyValue("ID", data.Binding.ID)
			output.KeyValue("Scope", kit.Ternary(data.Binding.EnvironmentID == nil, "global", cmp.Or(data.Binding.EnvironmentName, kit.FromPtr(data.Binding.EnvironmentID))))
			if data.Binding.Selector != nil {
				output.KeyValue("Selector", cmdutil.FormatSelector(*data.Binding.Selector))
			}
			for _, perm := range data.DroppedPermissions {
				output.Warning("Not granted (selector cannot scope it): %s", perm)
			}
			return nil
		},
	})
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <binding-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete a role binding",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Delete role binding %s?", args[0]),
				Path:           clitypes.RoleBinding(args[0]),
				FailureMessage: "failed to delete role binding",
				SuccessMessage: "Role binding deleted",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}

func newAccessCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "access <user-id>",
		Aliases:      []string{"effective"},
		Short:        "Show a user's effective access",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.EffectiveAccess]{
				Method:         http.MethodGet,
				Path:           clitypes.UserEffectiveAccess(args[0]),
				FailureMessage: "failed to get effective access",
				Render:         cmdutil.RenderEffectiveAccess,
			})
		},
	}
}
