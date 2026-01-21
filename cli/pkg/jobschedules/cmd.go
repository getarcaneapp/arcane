package jobschedules

import (
	"encoding/json"
	"fmt"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/jobschedule"
	"github.com/spf13/cobra"
)

var jsonOutput bool

// JobSchedulesCmd is the parent command for job schedule operations.
var JobSchedulesCmd = &cobra.Command{
	Use:     "job-schedules",
	Aliases: []string{"jobs", "job-schedule", "schedules"},
	Short:   "Manage background job schedules",
}

var getCmd = &cobra.Command{
	Use:          "get",
	Short:        "Get configured job schedule intervals",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.JobSchedules())
		if err != nil {
			return fmt.Errorf("failed to get job schedules: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var cfg jobschedule.Config
		if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			b, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(b))
			return nil
		}

		output.Header("Job Schedules")
		output.KeyValue("Environment health interval", cfg.EnvironmentHealthInterval)
		output.KeyValue("Event cleanup interval", cfg.EventCleanupInterval)
		output.KeyValue("Analytics heartbeat interval", cfg.AnalyticsHeartbeatInterval)
		return nil
	},
}

var (
	environmentHealthInterval  string
	eventCleanupInterval       string
	analyticsHeartbeatInterval string
)

var updateCmd = &cobra.Command{
	Use:          "update",
	Short:        "Update job schedule intervals",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		var req jobschedule.Update
		if cmd.Flags().Changed("environment-health-interval") {
			req.EnvironmentHealthInterval = &environmentHealthInterval
		}
		if cmd.Flags().Changed("event-cleanup-interval") {
			req.EventCleanupInterval = &eventCleanupInterval
		}
		if cmd.Flags().Changed("analytics-heartbeat-interval") {
			req.AnalyticsHeartbeatInterval = &analyticsHeartbeatInterval
		}

		if req.EnvironmentHealthInterval == nil && req.EventCleanupInterval == nil && req.AnalyticsHeartbeatInterval == nil {
			return fmt.Errorf("no updates provided (set at least one interval flag)")
		}

		resp, err := c.Put(cmd.Context(), types.Endpoints.JobSchedules(), req)
		if err != nil {
			return fmt.Errorf("failed to update job schedules: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[jobschedule.Config]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			b, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(b))
			return nil
		}

		output.Success("Job schedules updated")
		output.KeyValue("Environment health interval", result.Data.EnvironmentHealthInterval)
		output.KeyValue("Event cleanup interval", result.Data.EventCleanupInterval)
		output.KeyValue("Analytics heartbeat interval", result.Data.AnalyticsHeartbeatInterval)
		return nil
	},
}

func init() {
	JobSchedulesCmd.AddCommand(getCmd)
	JobSchedulesCmd.AddCommand(updateCmd)

	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	updateCmd.Flags().StringVar(&environmentHealthInterval, "environment-health-interval", "", "Environment health job interval (cron expression)")
	updateCmd.Flags().StringVar(&eventCleanupInterval, "event-cleanup-interval", "", "Event cleanup job interval (cron expression)")
	updateCmd.Flags().StringVar(&analyticsHeartbeatInterval, "analytics-heartbeat-interval", "", "Analytics heartbeat job interval (cron expression)")
}
