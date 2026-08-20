package jobs

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/jobschedule"
	"github.com/spf13/cobra"
)

var jsonOutput bool

// JobsCmd is the parent command for job schedule operations.
var JobsCmd = &cobra.Command{
	Use:     "jobs",
	Aliases: []string{"job"},
	Short:   "Manage background jobs",
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

		cfg, err := c.DoJSON[jobschedule.Config](cmd.Context(), http.MethodGet, types.JobSchedules(c.EnvID()), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to get job schedules")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(cfg)
		}

		output.Header("Job Schedules")
		output.KeyValue("Environment health interval", cfg.EnvironmentHealthInterval)
		output.KeyValue("Event cleanup interval", cfg.EventCleanupInterval)
		output.KeyValue("Expired sessions cleanup interval", cfg.ExpiredSessionsCleanupInterval)
		return nil
	},
}

var (
	environmentHealthInterval      string
	eventCleanupInterval           string
	expiredSessionsCleanupInterval string
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
		if cmd.Flags().Changed("expired-sessions-cleanup-interval") {
			req.ExpiredSessionsCleanupInterval = &expiredSessionsCleanupInterval
		}

		if req.EnvironmentHealthInterval == nil && req.EventCleanupInterval == nil && req.ExpiredSessionsCleanupInterval == nil {
			return errors.New("no updates provided (set at least one interval flag)")
		}

		result, err := c.PutJSON[jobschedule.Config](cmd.Context(), types.JobSchedules(c.EnvID()), req)
		if err != nil {
			return errors.WrapIf(err, "failed to update job schedules")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Job schedules updated")
		output.KeyValue("Environment health interval", result.Data.EnvironmentHealthInterval)
		output.KeyValue("Event cleanup interval", result.Data.EventCleanupInterval)
		output.KeyValue("Expired sessions cleanup interval", result.Data.ExpiredSessionsCleanupInterval)
		return nil
	},
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List background jobs",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		result, err := c.DoJSON[jobschedule.JobListResponse](cmd.Context(), http.MethodGet, types.Jobs(c.EnvID()), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to list jobs")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result)
		}

		headers := []string{"ID", "NAME", "CATEGORY", "SCHEDULE", "NEXT RUN", "ENABLED"}
		rows := make([][]string, len(result.Jobs))
		for i, job := range result.Jobs {
			nextRun := "-"
			if job.NextRun != nil {
				nextRun = job.NextRun.Format(time.RFC3339)
			}
			rows[i] = []string{
				job.ID,
				job.Name,
				job.Category,
				job.Schedule,
				nextRun,
				strconv.FormatBool(job.Enabled),
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d jobs\n", len(result.Jobs))
		return nil
	},
}

var runCmd = &cobra.Command{
	Use:          "run <job-id>",
	Short:        "Run a background job now",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		result, err := c.DoJSON[jobschedule.JobRunResponse](cmd.Context(), http.MethodPost, types.JobRun(c.EnvID(), args[0]), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to run job")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result)
		}

		if !result.Success {
			return errors.Errorf("job %s failed: %s", args[0], result.Message)
		}
		output.Success("%s", result.Message)
		return nil
	},
}

func init() {
	JobsCmd.AddCommand(getCmd)
	JobsCmd.AddCommand(updateCmd)
	JobsCmd.AddCommand(listCmd)
	JobsCmd.AddCommand(runCmd)

	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	updateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	runCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	updateCmd.Flags().StringVar(&environmentHealthInterval, "environment-health-interval", "", "Environment health job interval (cron expression)")
	updateCmd.Flags().StringVar(&eventCleanupInterval, "event-cleanup-interval", "", "Event cleanup job interval (cron expression)")
	updateCmd.Flags().StringVar(&expiredSessionsCleanupInterval, "expired-sessions-cleanup-interval", "", "Expired sessions cleanup job interval (cron expression)")
}
