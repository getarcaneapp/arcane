package jobs

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v3/internal/client"
	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the parent command for background jobs and their schedules.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "jobs",
		Aliases: []string{"job"},
		Short:   "Manage background jobs",
	}
	cmdutil.SetScope(cmdutil.ScopeResource, cmd)
	cmd.AddCommand(newSchedulesCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newRunCmd())
	return cmd
}

func newSchedulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedules",
		Short: "Manage job schedule intervals",
	}
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newUpdateCmd())
	return cmd
}

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "get",
		Short:        "Get configured job schedule intervals",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			cfg, err := c.DoJSON[types.JobScheduleConfig](cmd.Context(), http.MethodGet, clitypes.JobSchedules(c.EnvID()), nil)
			if err != nil {
				return fmt.Errorf("failed to get job schedules: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(cfg)
			}

			output.Header("Job Schedules")
			output.KeyValue("Environment health interval", cfg.EnvironmentHealthInterval)
			output.KeyValue("Event cleanup interval", cfg.EventCleanupInterval)
			output.KeyValue("Expired sessions cleanup interval", cfg.ExpiredSessionsCleanupInterval)
			return nil
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newUpdateCmd() *cobra.Command {
	var opts struct {
		environmentHealthInterval      string
		eventCleanupInterval           string
		expiredSessionsCleanupInterval string
	}
	cmd := &cobra.Command{
		Use:          "update",
		Short:        "Update job schedule intervals",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := map[string]any{}
			for _, field := range []struct {
				flag  string
				key   string
				value string
			}{
				{flag: "environment-health-interval", key: "environmentHealthInterval", value: opts.environmentHealthInterval},
				{flag: "event-cleanup-interval", key: "eventCleanupInterval", value: opts.eventCleanupInterval},
				{flag: "expired-sessions-cleanup-interval", key: "expiredSessionsCleanupInterval", value: opts.expiredSessionsCleanupInterval},
			} {
				if cmd.Flags().Changed(field.flag) {
					req[field.key] = field.value
				}
			}
			if len(req) == 0 {
				return errors.New("no updates provided (set at least one interval flag)")
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.JobScheduleConfig]{
				Method:         http.MethodPatch,
				Path:           clitypes.JobSchedules(c.EnvID()),
				Body:           req,
				FailureMessage: "failed to update job schedules",
				Render: func(data types.JobScheduleConfig) error {
					output.Success("Job schedules updated")
					output.KeyValue("Environment health interval", data.EnvironmentHealthInterval)
					output.KeyValue("Event cleanup interval", data.EventCleanupInterval)
					output.KeyValue("Expired sessions cleanup interval", data.ExpiredSessionsCleanupInterval)
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeFleet, cmd)
	cmd.Flags().StringVar(&opts.environmentHealthInterval, "environment-health-interval", "", "Environment health job interval (cron expression)")
	cmd.Flags().StringVar(&opts.eventCleanupInterval, "event-cleanup-interval", "", "Event cleanup job interval (cron expression)")
	cmd.Flags().StringVar(&opts.expiredSessionsCleanupInterval, "expired-sessions-cleanup-interval", "", "Expired sessions cleanup job interval (cron expression)")
	return cmd
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List background jobs",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			result, err := c.DoJSON[types.JobListResponse](cmd.Context(), http.MethodGet, clitypes.Jobs(c.EnvID()), nil)
			if err != nil {
				return fmt.Errorf("failed to list jobs: %w", err)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
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

			return cmdutil.PrintTable(cmd, headers, rows, cmdutil.TableSummary{Resource: "jobs"})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	return cmd
}

func newRunCmd() *cobra.Command {
	var opts struct {
		requestID string
	}
	cmd := &cobra.Command{
		Use:          "run <job-id>",
		Short:        "Queue a background job now",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			id := opts.requestID
			if id == "" {
				var value [16]byte
				_, _ = rand.Read(value[:])
				value[6] = (value[6] & 0x0f) | 0x40
				value[8] = (value[8] & 0x3f) | 0x80
				id = fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
			}
			result, err := c.DoJSON[types.JobRunResponse](cmd.Context(), http.MethodPost, clitypes.JobAction(c.EnvID(), args[0], "run"), nil, http.Header{"Idempotency-Key": []string{id}})
			if err != nil {
				return fmt.Errorf("failed to queue job (reuse --request-id %s if delivery is uncertain): %w", id, err)
			}

			if !result.Success {
				return fmt.Errorf("job %s failed: %s", args[0], result.Message)
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(result)
			}
			output.Success("%s", result.Message)
			output.KeyValue("Run ID", result.RunID)
			output.KeyValue("Status", string(result.Status))
			return nil
		},
	}
	cmdutil.SetResource(jobRef, cmd)
	cmd.Flags().StringVar(&opts.requestID, "request-id", "", "Stable request ID to reuse when retrying an uncertain submission")
	return cmd
}

var jobRef = cmdutil.ResourceRef[types.JobStatus, types.JobStatus]{
	EnvironmentScoped: true, Singular: "job", Plural: "jobs", IDHint: "the job ID", ListCmd: "arcane-cli jobs list",
	SearchCandidates: func(ctx context.Context, c *client.Client, identifier string) ([]types.JobStatus, error) {
		result, err := c.DoJSON[types.JobListResponse](ctx, http.MethodGet, clitypes.Jobs(c.EnvID()), nil)
		if err != nil {
			return nil, err
		}
		return slices.DeleteFunc(result.Jobs, func(item types.JobStatus) bool { return !strings.EqualFold(item.ID, identifier) }), nil
	},
	IdentifierOf: func(item types.JobStatus) string { return item.ID },
	Label:        func(item types.JobStatus) string { return item.Name + " (" + item.ID + ")" },
	Promote:      func(item types.JobStatus) *types.JobStatus { return &item },
}
