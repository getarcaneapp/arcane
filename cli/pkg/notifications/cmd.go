package notifications

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/notification"
)

var jsonOutput bool

// NotificationsCmd is the parent command for notification operations
var NotificationsCmd = &cobra.Command{
	Use:     "notifications",
	Aliases: []string{"notif", "notify"},
	Short:   "Manage notifications",
}

var appriseGetCmd = &cobra.Command{
	Use:          "apprise-get",
	Short:        "Get Apprise configuration",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.NotificationsApprise(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get apprise config: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[notification.AppriseResponse]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Apprise Configuration")
		output.KeyValue("ID", fmt.Sprintf("%d", result.Data.ID))
		output.KeyValue("API URL", result.Data.APIURL)
		output.KeyValue("Enabled", fmt.Sprintf("%t", result.Data.Enabled))
		output.KeyValue("Image Update Tag", result.Data.ImageUpdateTag)
		output.KeyValue("Container Update Tag", result.Data.ContainerUpdateTag)
		return nil
	},
}

var settingsGetCmd = &cobra.Command{
	Use:          "settings-get",
	Short:        "Get notification settings",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.NotificationsSettings(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get settings: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]notification.Response]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"ID", "PROVIDER", "ENABLED"}
		rows := make([][]string, len(result.Data))
		for i, setting := range result.Data {
			rows[i] = []string{
				fmt.Sprintf("%d", setting.ID),
				string(setting.Provider),
				fmt.Sprintf("%t", setting.Enabled),
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d notification settings\n", len(result.Data))
		return nil
	},
}

func init() {
	NotificationsCmd.AddCommand(appriseGetCmd)
	NotificationsCmd.AddCommand(settingsGetCmd)

	appriseGetCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	settingsGetCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
