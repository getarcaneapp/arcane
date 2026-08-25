package notifications

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/notification"
	"github.com/spf13/cobra"
)

var (
	jsonOutput     bool
	notifForceFlag bool
)

// NotificationsCmd is the parent command for notification operations
var NotificationsCmd = &cobra.Command{
	Use:     "notifications",
	Aliases: []string{"notif", "notify"},
	Short:   "Manage notifications",
}

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Manage notification settings",
}

var settingsGetCmd = &cobra.Command{
	Use:          "get",
	Short:        "Get notification settings",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.NotificationsSettings(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to get settings")
		}
		defer func() { _ = resp.Body.Close() }()

		var result []notification.Response
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
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

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d notification settings\n", len(result))
		return nil
	},
}

var settingsDeleteCmd = &cobra.Command{
	Use:          "delete <provider>",
	Short:        "Delete notification provider settings",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !notifForceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete notification settings for %s?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.NotificationSettingsProvider(c.EnvID(), args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to delete notification settings")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to delete notification settings")
		}

		output.Success("Notification settings for %s deleted successfully", args[0])
		return nil
	},
}

var (
	setProvider string
	setEnabled  bool
	setConfig   string
	setFile     string
)

var settingsSetCmd = &cobra.Command{
	Use:          "set",
	Short:        "Create or update notification provider settings",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		var req notification.Update
		if setFile != "" {
			data, err := os.ReadFile(setFile)
			if err != nil {
				return errors.WrapIf(err, "failed to read settings file")
			}
			if err := json.Unmarshal(data, &req); err != nil {
				return errors.WrapIf(err, "failed to parse settings file")
			}
		}

		if cmd.Flags().Changed("provider") {
			req.Provider = notification.Provider(setProvider)
		}
		if cmd.Flags().Changed("enabled") {
			req.Enabled = setEnabled
		}
		if cmd.Flags().Changed("config") {
			if err := json.Unmarshal([]byte(setConfig), &req.Config); err != nil {
				return errors.WrapIf(err, "failed to parse --config JSON")
			}
		}

		if req.Provider == "" {
			return errors.New("provider is required (use --provider or --file)")
		}
		if req.Config == nil {
			return errors.New("config is required (use --config or --file)")
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.NotificationsSettings(c.EnvID()), req)
		if err != nil {
			return errors.WrapIf(err, "failed to save notification settings")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "failed to save notification settings")
		}

		var result notification.Response
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return err
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result)
		}

		output.Success("Notification settings for %s saved successfully", result.Provider)
		output.KeyValue("ID", strconv.FormatUint(uint64(result.ID), 10))
		output.KeyValue("Enabled", strconv.FormatBool(result.Enabled))
		return nil
	},
}

var testProviderCmd = &cobra.Command{
	Use:          "test <provider>",
	Short:        "Test a notification provider",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.NotificationsTestProvider(c.EnvID(), args[0]), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to test notification provider")
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return errors.WrapIf(err, "notification test failed")
		}

		if jsonOutput {
			var result base.ApiResponse[any]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("Notification test for %s successful", args[0])
		return nil
	},
}

func init() {
	NotificationsCmd.AddCommand(settingsCmd)
	NotificationsCmd.AddCommand(testProviderCmd)

	settingsCmd.AddCommand(settingsGetCmd)
	settingsCmd.AddCommand(settingsSetCmd)
	settingsCmd.AddCommand(settingsDeleteCmd)

	settingsGetCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	settingsSetCmd.Flags().StringVar(&setProvider, "provider", "", "Notification provider (e.g. discord, email, telegram)")
	settingsSetCmd.Flags().BoolVar(&setEnabled, "enabled", false, "Enable the provider")
	settingsSetCmd.Flags().StringVar(&setConfig, "config", "", "Provider configuration as a JSON object")
	settingsSetCmd.Flags().StringVar(&setFile, "file", "", "Path to a JSON file with provider, enabled, and config fields")
	settingsSetCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	settingsDeleteCmd.Flags().BoolVarP(&notifForceFlag, "force", "f", false, "Force deletion without confirmation")
	settingsDeleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	testProviderCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
