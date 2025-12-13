package updater

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/updater"
)

var jsonOutput bool

// UpdaterCmd is the parent command for updater operations
var UpdaterCmd = &cobra.Command{
	Use:     "updater",
	Aliases: []string{"upd"},
	Short:   "Auto-updater operations",
}

var statusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Get updater status",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.UpdaterStatus(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get updater status: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[updater.Status]
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

		output.Header("Updater Status")
		output.KeyValue("Updating Containers", fmt.Sprintf("%d", result.Data.UpdatingContainers))
		output.KeyValue("Updating Projects", fmt.Sprintf("%d", result.Data.UpdatingProjects))
		return nil
	},
}

var runCmd = &cobra.Command{
	Use:          "run",
	Short:        "Run updater",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.UpdaterRun(c.EnvID()), nil)
		if err != nil {
			return fmt.Errorf("failed to run updater: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[updater.Result]
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

		output.Header("Updater Results")
		output.KeyValue("Checked", fmt.Sprintf("%d", result.Data.Checked))
		output.KeyValue("Updated", fmt.Sprintf("%d", result.Data.Updated))
		output.KeyValue("Skipped", fmt.Sprintf("%d", result.Data.Skipped))
		output.KeyValue("Failed", fmt.Sprintf("%d", result.Data.Failed))
		output.KeyValue("Duration", result.Data.Duration)
		return nil
	},
}

var historyCmd = &cobra.Command{
	Use:          "history",
	Short:        "Get updater history",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.UpdaterHistory(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get updater history: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[[]updater.Result]
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

		headers := []string{"CHECKED", "UPDATED", "FAILED", "DURATION"}
		rows := make([][]string, len(result.Data))
		for i, h := range result.Data {
			rows[i] = []string{
				fmt.Sprintf("%d", h.Checked),
				fmt.Sprintf("%d", h.Updated),
				fmt.Sprintf("%d", h.Failed),
				h.Duration,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d history entries\n", len(result.Data))
		return nil
	},
}

func init() {
	UpdaterCmd.AddCommand(statusCmd)
	UpdaterCmd.AddCommand(runCmd)
	UpdaterCmd.AddCommand(historyCmd)

	statusCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	runCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	historyCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
