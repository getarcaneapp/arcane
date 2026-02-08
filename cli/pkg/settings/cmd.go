package settings

import (
	"encoding/json"
	"fmt"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/settings"
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
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
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

func init() {
	SettingsCmd.AddCommand(listCmd)

	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
