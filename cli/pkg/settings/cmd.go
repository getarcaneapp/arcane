package settings

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/settings"
	"github.com/spf13/cobra"
)

var jsonOutput bool

var Cmd = &cobra.Command{
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
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.Settings(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get settings: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result []settings.PublicSetting
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result)
		}

		headers := []string{"KEY", "TYPE", "VALUE"}
		rows := make([][]string, len(result))
		for i, s := range result {
			rows[i] = []string{
				s.Key,
				s.Type,
				s.Value,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d settings\n", len(result))
		return nil
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
			return fmt.Errorf("failed to read file %s: %w", settingsUpdateFile, err)
		}

		var req settings.Update
		if err := json.Unmarshal(data, &req); err != nil {
			return fmt.Errorf("failed to parse settings file: %w", err)
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.Settings(c.EnvID()), req)
		if err != nil {
			return fmt.Errorf("failed to update settings: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if err := cmdutil.EnsureSuccessStatus(resp); err != nil {
			return fmt.Errorf("failed to update settings: %w", err)
		}

		if jsonOutput {
			var result any
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			return cmdutil.PrintJSON(result)
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
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.SettingsPublic(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get public settings: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result []settings.PublicSetting
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
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
		fmt.Printf("\nTotal: %d public settings\n", len(result))
		return nil
	},
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(updateCmd)
	Cmd.AddCommand(publicCmd)

	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	updateCmd.Flags().StringVarP(&settingsUpdateFile, "file", "f", "", "Settings JSON file")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	_ = updateCmd.MarkFlagRequired("file")

	publicCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
