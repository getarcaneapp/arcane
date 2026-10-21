package notifications

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command is the parent command for notification operations
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "notifications",
		Aliases: []string{"notif", "notify"},
		Short:   "Manage notifications",
	}
	cmdutil.SetScope(cmdutil.ScopeResource, command)
	command.AddCommand(newSettingsCmd())
	command.AddCommand(newTestProviderCmd())
	return command
}

func newSettingsCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   "settings",
		Short: "Manage notification settings",
	}
	command.AddCommand(newSettingsGetCmd())
	command.AddCommand(newSettingsSetCmd())
	command.AddCommand(newSettingsDeleteCmd())
	return command
}

func newSettingsGetCmd() *cobra.Command {
	command := &cobra.Command{
		Use:          "get",
		Short:        "Get notification settings",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			body, err := c.DoRaw(cmd.Context(), http.MethodGet, clitypes.NotificationsSettings(c.EnvID()), nil)
			if err != nil {
				return fmt.Errorf("failed to get settings: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintRawJSON(body)
			}

			var result []types.NotificationSettings
			if decodeErr := json.Unmarshal(body, &result); decodeErr != nil {
				return fmt.Errorf("failed to get settings: %w", decodeErr)
			}

			headers := []string{"ID", "PROVIDER", "ENABLED"}
			rows := make([][]string, len(result))
			for i, setting := range result {
				rows[i] = []string{
					strconv.FormatUint(uint64(setting.ID), 10),
					string(setting.Provider),
					strconv.FormatBool(setting.Enabled),
				}
			}

			if tableErr := cmdutil.PrintTable(cmd, headers, rows, cmdutil.TableSummary{Resource: "notification settings"}); tableErr != nil {
				return tableErr
			}
			return nil
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, command)
	return command
}

func newSettingsDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <provider>",
		Short:        "Delete notification provider settings",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete notification settings for %s?", args[0]),
				Path:           clitypes.NotificationSettingsProvider(c.EnvID(), args[0]),
				FailureMessage: "failed to delete notification settings",
				SuccessMessage: fmt.Sprintf("Notification settings for %s deleted successfully", args[0]),
			})
		},
	}
	cmdutil.SetResource(providerRef, command)
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}

func newSettingsSetCmd() *cobra.Command {
	var opts struct {
		provider types.NotificationProvider
		enabled  bool
		config   string
		file     string
	}
	command := &cobra.Command{
		Use:   "set",
		Short: "Create or update notification provider settings",
		Example: "  arcane admin notifications settings set --file provider.json\n" +
			"  arcane admin notifications settings set --provider generic --enabled --provider-config '{\"webhookUrl\":\"https://example.com/notify\"}'",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var req types.NotificationSettings
			if opts.file != "" {
				data, err := os.ReadFile(opts.file)
				if err != nil {
					return fmt.Errorf("failed to read settings file: %w", err)
				}
				if unmarshalErr := json.Unmarshal(data, &req); unmarshalErr != nil {
					return fmt.Errorf("failed to parse settings file: %w", unmarshalErr)
				}
			}

			if cmd.Flags().Changed("provider") {
				req.Provider = opts.provider
			}
			if cmd.Flags().Changed("enabled") {
				req.Enabled = opts.enabled
			}
			// TODO(v3): drop the deprecated flag handling.
			if cmd.Flags().Changed("provider-config") || cmd.Flags().Changed("config") {
				configFlag := "--config"
				if cmd.Flags().Changed("provider-config") {
					configFlag = "--provider-config"
				}
				if configErr := json.Unmarshal([]byte(opts.config), &req.Config); configErr != nil {
					return fmt.Errorf("failed to parse %s JSON: %w", configFlag, configErr)
				}
			}

			if req.Provider == "" {
				return errors.New("provider is required (use --provider or --file)")
			}
			if req.Config == nil {
				return errors.New("config is required (use --provider-config or --file)")
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			body, err := c.DoRaw(cmd.Context(), http.MethodPost, clitypes.NotificationsSettings(c.EnvID()), req)
			if err != nil {
				return fmt.Errorf("failed to save notification settings: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintRawJSON(body)
			}

			var result types.NotificationSettings
			if decodeErr := json.Unmarshal(body, &result); decodeErr != nil {
				return fmt.Errorf("failed to save notification settings: %w", decodeErr)
			}
			output.Success("Notification settings for %s saved successfully", result.Provider)
			output.KeyValue("ID", strconv.FormatUint(uint64(result.ID), 10))
			output.KeyValue("Enabled", strconv.FormatBool(result.Enabled))
			return nil
		},
	}
	cmdutil.SetScope(cmdutil.ScopeFleet, command)
	cmdutil.EnumVarP(command, command.Flags(), &opts.provider, "provider", "", "", "Notification provider",
		types.NotificationProviderDiscord, types.NotificationProviderEmail, types.NotificationProviderTelegram,
		types.NotificationProviderSignal, types.NotificationProviderSlack, types.NotificationProviderNtfy,
		types.NotificationProviderPushover, types.NotificationProviderGotify, types.NotificationProviderMatrix,
		types.NotificationProviderGoogleChat, types.NotificationProviderGeneric)
	command.Flags().BoolVar(&opts.enabled, "enabled", false, "Enable the provider")
	command.Flags().StringVar(&opts.config, "provider-config", "", "Provider configuration as a JSON object")
	command.Flags().StringVar(&opts.config, "config", "", "Provider configuration as a JSON object")
	// TODO(v3): remove the deprecated --config flag.
	_ = command.Flags().MarkDeprecated("config", "use --provider-config")
	command.Flags().StringVar(&opts.file, "file", "", "Path to a JSON file with provider, enabled, and config fields")
	return command
}

func newTestProviderCmd() *cobra.Command {
	command := &cobra.Command{
		Use:          "test <provider>",
		Short:        "Test a notification provider",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.NotificationTestResponse]{
				Method:         http.MethodPost,
				Path:           clitypes.NotificationsTestProvider(c.EnvID(), args[0]),
				FailureMessage: "failed to test notification provider",
				Render: func(types.NotificationTestResponse) error {
					output.Success("%s", fmt.Sprintf("Notification test for %s successful", args[0]))
					return nil
				},
			})
		},
	}
	cmdutil.SetResource(providerRef, command)
	return command
}

var providerRef = cmdutil.ResourceRef[types.NotificationSettings, types.NotificationSettings]{
	EnvironmentScoped: true,
	Singular:          "provider", Plural: "providers",
	IDHint: "the identifier", ListCmd: "arcane-cli admin notifications settings get",
	GetPath: clitypes.NotificationSettingsProvider, ListPath: clitypes.NotificationsSettings,
	Matches: func(item types.NotificationSettings, lower, original string) bool {
		return strings.EqualFold(string(item.Provider), original)
	},
	Label:        func(item types.NotificationSettings) string { return string(item.Provider) },
	Promote:      func(item types.NotificationSettings) *types.NotificationSettings { return &item },
	IdentifierOf: func(item types.NotificationSettings) string { return string(item.Provider) },
}
