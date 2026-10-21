package environments

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/config"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/prompt"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

var (
	statusOnlineStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	statusOfflineStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	statusMutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#94a3b8"))
	enabledStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#6d28d9"))
)

// Command is the parent command for environment operations
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "environments",
		Aliases: []string{"environment", "env", "e"},
		Short:   "Manage environments",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newGetCmd())
	command.AddCommand(newTestCmd())
	command.AddCommand(newDeleteCmd())
	command.AddCommand(newSwitchCmd())
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

func newSwitchCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "switch",
		Short:        "Switch the default environment (interactive)",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmdutil.AllowPrompt(cmd) {
				return errors.New("interactive terminal required; run `arcane config set environment <id>` instead")
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			path := fmt.Sprintf("%s?limit=%d", clitypes.Environments(), cmdutil.ShowAllLimit)
			result, err := c.GetJSON[[]types.Environment](cmd.Context(), path)
			if err != nil {
				return fmt.Errorf("failed to list environments: %w", err)
			}

			if len(result.Data) == 0 {
				return errors.New("no environments available")
			}

			currentEnv := cmp.Or(strings.TrimSpace(cfg.DefaultEnvironment), "0")

			envs := result.Data
			sort.SliceStable(envs, func(i, j int) bool {
				left := strings.ToLower(cmp.Or(strings.TrimSpace(envs[i].Name), envs[i].ID))
				right := strings.ToLower(cmp.Or(strings.TrimSpace(envs[j].Name), envs[j].ID))
				if left == right {
					return envs[i].ID < envs[j].ID
				}
				return left < right
			})

			choice, err := prompt.Select("environment", environmentOptions(envs, currentEnv))
			if err != nil {
				return err
			}

			selected := envs[choice]
			if selected.ID == currentEnv {
				output.Info("Default environment already set to %s", selected.ID)
				return nil
			}

			cfg.DefaultEnvironment = selected.ID
			if saveErr := config.Save(cmd.Context(), cfg); saveErr != nil {
				return fmt.Errorf("failed to save config: %w", saveErr)
			}

			output.Success("Default environment set to %s", selected.ID)
			if strings.TrimSpace(selected.Name) != "" {
				output.KeyValue("Name", selected.Name)
			}
			output.KeyValue("API URL", selected.ApiUrl)
			return nil
		},
	}
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		name    string
		apiUrl  string
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
			if cmd.Flags().Changed("api-url") {
				req["apiUrl"] = opts.apiUrl
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
	command.Flags().StringVar(&opts.apiUrl, "api-url", "", "API URL")
	command.Flags().BoolVar(&opts.enabled, "enabled", false, "Enable environment")
	return command
}

func newCreateCmd() *cobra.Command {
	var opts struct {
		name        string
		apiUrl      string
		accessToken string
		useApiKey   bool
		edge        bool
		enabled     bool
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

			req := types.Environment{
				Name:        opts.name,
				ApiUrl:      opts.apiUrl,
				Enabled:     opts.enabled,
				IsEdge:      opts.edge,
				AccessToken: opts.accessToken,
				UseApiKey:   opts.useApiKey,
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Environment]{
				Method:         http.MethodPost,
				Path:           clitypes.Environments(),
				FailureMessage: "failed to create environment",
				Body:           req,
				Render: func(data types.Environment) error {
					output.Success("Environment created successfully")
					output.KeyValue("ID", data.ID)
					output.KeyValue("Name", data.Name)
					output.KeyValue("API URL", data.ApiUrl)
					output.KeyValue("Enabled", data.Enabled)
					if data.ApiKey != nil && *data.ApiKey != "" {
						output.Warning("Pairing API key (shown only once) — store it now:")
						output.KeyValue("API Key", *data.ApiKey)
					}
					return nil
				},
			})
		},
	}
	command.Flags().StringVar(&opts.apiUrl, "api-url", "", "API URL of the environment (required)")
	command.Flags().StringVar(&opts.name, "name", "", "Environment name")
	command.Flags().StringVar(&opts.accessToken, "access-token", "", "Access token for legacy pairing")
	command.Flags().BoolVar(&opts.useApiKey, "use-api-key", false, "Pair using a generated API key (shown once)")
	command.Flags().BoolVar(&opts.edge, "edge", false, "Create as an edge environment")
	command.Flags().BoolVar(&opts.enabled, "enabled", true, "Enable environment")
	_ = command.MarkFlagRequired("api-url")
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

func environmentOptions(environments []types.Environment, currentEnv string) []string {
	options := make([]string, len(environments))
	for i, env := range environments {
		displayName := cmp.Or(strings.TrimSpace(env.Name), env.ID)
		status := cmp.Or(strings.TrimSpace(env.Status), "unknown")
		var statusLabel string
		switch strings.ToLower(status) {
		case "online":
			statusLabel = statusOnlineStyle.Render(status)
		case "offline":
			statusLabel = statusOfflineStyle.Render(status)
		default:
			statusLabel = statusMutedStyle.Render(status)
		}

		enabledLabel := statusMutedStyle.Render("disabled")
		if env.Enabled {
			enabledLabel = enabledStyle.Render("enabled")
		}
		marker := "  "
		if env.ID == currentEnv {
			marker = "* "
		}
		options[i] = fmt.Sprintf("%s%s (id: %s, %s, %s)", marker, displayName, env.ID, statusLabel, enabledLabel)
	}
	return options
}
