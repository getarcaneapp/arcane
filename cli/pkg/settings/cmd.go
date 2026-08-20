package settings

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"os"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/settings"
	"github.com/spf13/cobra"
)

var jsonOutput bool

// SettingsCmd is the parent command for settings operations
var SettingsCmd = &cobra.Command{
	Use:     "settings",
	Aliases: []string{"setting"},
	Short:   "Manage settings",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List environment settings",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSettingsList(cmd, settingsListConfig{
			endpoint:       types.Settings,
			failureMessage: "failed to get settings",
			totalLabel:     "settings",
		})
	},
}

var settingsUpdateFile string

var updateCmd = &cobra.Command{
	Use:          "update",
	Short:        "Update environment settings",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		data, err := os.ReadFile(settingsUpdateFile)
		if err != nil {
			return errors.WrapIff(err, "failed to read file %s", settingsUpdateFile)
		}

		var req settings.Update
		if err := json.Unmarshal(data, &req); err != nil {
			return errors.WrapIf(err, "failed to parse settings file")
		}

		result, err := c.PutJSON[[]settings.SettingDto](cmd.Context(), types.Settings(c.EnvID()), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update settings")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Settings updated successfully")
		return nil
	},
}

var publicCmd = &cobra.Command{
	Use:          "public",
	Short:        "List public settings",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSettingsList(cmd, settingsListConfig{
			endpoint:       types.SettingsPublic,
			failureMessage: "failed to get public settings",
			totalLabel:     "public settings",
		})
	},
}

type settingsListConfig struct {
	endpoint       func(string) string
	failureMessage string
	totalLabel     string
}

func runSettingsList(cmd *cobra.Command, cfg settingsListConfig) error {
	c, err := client.NewFromConfig()
	if err != nil {
		return err
	}

	result, err := c.DoJSON[[]settings.PublicSetting](cmd.Context(), http.MethodGet, cfg.endpoint(c.EnvID()), nil)
	if err != nil {
		return errors.WrapIff(err, "%s", cfg.failureMessage)
	}

	if jsonOutput {
		return cmdutil.PrintJSON(result)
	}

	headers := []string{"KEY", "TYPE", "VALUE"}
	rows := make([][]string, len(result))
	for i, s := range result {
		rows[i] = []string{s.Key, s.Type, s.Value}
	}

	output.Table(headers, rows)
	fmt.Printf("\nTotal: %d %s\n", len(result), cfg.totalLabel)
	return nil
}

func init() {
	SettingsCmd.AddCommand(listCmd)
	SettingsCmd.AddCommand(updateCmd)
	SettingsCmd.AddCommand(publicCmd)

	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	updateCmd.Flags().StringVarP(&settingsUpdateFile, "file", "f", "", "Settings JSON file")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = updateCmd.MarkFlagRequired("file")

	publicCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
