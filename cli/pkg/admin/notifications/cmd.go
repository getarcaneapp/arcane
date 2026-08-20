package notifications

import (
	"fmt"
	"net/http"
	"strconv"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
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

		result, err := c.DoJSON[[]notification.Response](cmd.Context(), http.MethodGet, types.Endpoints.NotificationsSettings(c.EnvID()), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to get settings")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result)
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.NotificationSettingsProvider(c.EnvID(), args[0]))
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

		return cmdutil.RunPostAction[notification.TestResponse](cmd, c, cmdutil.PostActionSpec{
			Path:           types.Endpoints.NotificationsTestProvider(c.EnvID(), args[0]),
			FailureMessage: "failed to test notification provider",
			SuccessMessage: fmt.Sprintf("Notification test for %s successful", args[0]),
			JSON:           jsonOutput,
		})
	},
}

func init() {
	NotificationsCmd.AddCommand(settingsCmd)
	NotificationsCmd.AddCommand(testProviderCmd)

	settingsCmd.AddCommand(settingsGetCmd)
	settingsCmd.AddCommand(settingsDeleteCmd)

	settingsGetCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	settingsDeleteCmd.Flags().BoolVarP(&notifForceFlag, "force", "f", false, "Force deletion without confirmation")
	settingsDeleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	testProviderCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
