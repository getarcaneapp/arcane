package environments

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the parent command for environment operations
func Command() *cobra.Command {
	command := &cobra.Command{
		Annotations: map[string]string{"arcane:scope": cmdutil.ScopeGlobal},
		Use:         "environments",
		Aliases:     []string{"environment", "env", "e"},
		Short:       "Manage environments",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newGetCmd())
	command.AddCommand(newTestCmd())
	command.AddCommand(newDeleteCmd())
	command.AddCommand(newUpdateCmd())
	command.AddCommand(newVersionCmd())
	command.AddCommand(newCreateCmd())
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
		Short:        "List environments",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.Environment]{
				Resource: "environments",
				Endpoint: clitypes.Environments(),
				Params:   cmdutil.ListParams{Resource: "environments", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "NAME", "API URL", "STATUS", "ENABLED"},
				Row: func(env types.Environment) []string {
					return []string{
						env.ID,
						env.Name,
						env.ApiUrl,
						env.Status,
						strconv.FormatBool(env.Enabled),
					}
				},
			})
		},
	}
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of environments to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete environment",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete environment %s?", args[0]),
				Path:           clitypes.Environment(args[0]),
				FailureMessage: "failed to delete environment",
				SuccessMessage: "Environment deleted successfully",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <id>",
		Short:        "Get environment details",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Environment]{
				Method:         http.MethodGet,
				Path:           clitypes.Environment(args[0]),
				FailureMessage: "failed to get environment",
				Body:           nil,
				Render: func(data types.Environment) error {
					output.Header("Environment Details")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					output.KeyValue("API URL", data.ApiUrl)
					output.KeyValue("Status", data.Status)
					output.KeyValue("Enabled", data.Enabled)
					return nil
				},
			})
		},
	}
}

func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "test <id>",
		Short:        "Test environment connection",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.EnvironmentTest]{
				Method:         http.MethodPost,
				Path:           clitypes.EnvironmentTest(args[0]),
				FailureMessage: "failed to test environment",
				Render: func(types.EnvironmentTest) error {
					output.Success("%s", "Environment connection test successful")
					return nil
				},
			})
		},
	}
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		name    string
		enabled bool
	}
	command := &cobra.Command{
		Use:          "update <id>",
		Short:        "Update environment",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			req := map[string]any{}
			if cmd.Flags().Changed("name") {
				req["name"] = opts.name
			}
			if cmd.Flags().Changed("enabled") {
				req["enabled"] = opts.enabled
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Environment]{
				Method:         http.MethodPatch,
				Path:           clitypes.Environment(args[0]),
				FailureMessage: "failed to update environment",
				Body:           req,
				Render: func(data types.Environment) error {
					output.Success("Environment updated successfully")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					output.KeyValue("API URL", data.ApiUrl)
					output.KeyValue("Enabled", data.Enabled)
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.name, "name", "", "Environment name")
	command.Flags().BoolVar(&opts.enabled, "enabled", false, "Enable environment")
	return command
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		name    string
		enabled bool
	}
	command := &cobra.Command{
		Use:          "create",
		Short:        "Create environment",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Environment]{
				Method:         http.MethodPost,
				Path:           clitypes.Environments(),
				FailureMessage: "failed to create environment",
				Body: map[string]any{
					"name":    opts.name,
					"enabled": opts.enabled,
				},
				Render: func(data types.Environment) error {
					output.Success("Environment created successfully")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					output.KeyValue("API URL", data.ApiUrl)
					output.KeyValue("Enabled", data.Enabled)
					output.Warning("Agent API key (shown only once) — store it now:")
					output.KeyValue("API Key", kit.FromPtr(data.ApiKey))

					snippets, snippetsErr := c.GetJSON[types.DeploymentSnippet](cmd.Context(), clitypes.EnvironmentDeployment(data.ID))
					if snippetsErr != nil {
						return fmt.Errorf("failed to get deployment snippets: %w", snippetsErr)
					}
					output.Header("Docker Run")
					_, _ = fmt.Fprintf(output.Stdout(), "\n%s\n", snippets.Data.DockerRun)
					output.Header("Docker Compose")
					_, _ = fmt.Fprintf(output.Stdout(), "\n%s\n", snippets.Data.DockerCompose)
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.name, "name", "", "Environment name (required)")
	command.Flags().BoolVar(&opts.enabled, "enabled", true, "Enable environment")
	_ = command.MarkFlagRequired("name")
	return command
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "version <id>",
		Short:        "Get environment version",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.VersionInfo]{
				Method:         http.MethodGet,
				Path:           clitypes.EnvironmentVersion(args[0]),
				FailureMessage: "failed to get environment version",
				Render: func(data types.VersionInfo) error {
					output.Header("Environment Version")
					output.KeyValue("Version", data.DisplayVersion)
					output.KeyValue("Full Version", data.CurrentVersion)
					output.KeyValue("Tag", data.CurrentTag)
					output.KeyValue("Revision", data.ShortRevision)
					output.KeyValue("Go Version", data.GoVersion)
					output.KeyValue("Build Time", data.BuildTime)
					output.KeyValue("Update Available", strconv.FormatBool(data.UpdateAvailable))
					return nil
				},
			})
		},
	}
}
