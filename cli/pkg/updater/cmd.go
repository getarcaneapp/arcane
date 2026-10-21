package updater

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/runtime"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command is the parent command for updater operations
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "updater",
		Aliases: []string{"upd"},
		Short:   "Auto-updater operations",
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	cmd.AddCommand(newStatusCmd(), newRunCmd(), newHistoryCmd())
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Get updater status",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.UpdaterStatus]{
				Method:         http.MethodGet,
				Path:           clitypes.UpdaterStatus(c.EnvID()),
				FailureMessage: "failed to get updater status",
				Render: func(data types.UpdaterStatus) error {
					output.Header("Updater Status")
					output.KeyValue("Updating Containers", strconv.Itoa(data.UpdatingContainers))
					output.KeyValue("Updating Projects", strconv.Itoa(data.UpdatingProjects))
					return nil
				},
			})
		},
	}
}

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "run",
		Short:        "Run updater",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			// Updater run can take a long time as it pulls images and restarts containers
			c.SetTimeout(30 * time.Minute)

			result, err := c.PostJSON[types.UpdaterResult](cmd.Context(), clitypes.UpdaterRun(c.EnvID()), nil)
			if err != nil {
				return fmt.Errorf("failed to run updater: %w", err)
			}

			if app, ok := runtime.From(cmd.Context()); ok && app.FleetConfirmed && result.Data.Failed > 0 {
				return fmt.Errorf("updater failed for %d resources", result.Data.Failed)
			}

			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintJSON(result.Data)
			}

			output.Header("Updater Results")
			output.KeyValue("Checked", strconv.Itoa(result.Data.Checked))
			output.KeyValue("Updated", strconv.Itoa(result.Data.Updated))
			output.KeyValue("Skipped", strconv.Itoa(result.Data.Skipped))
			output.KeyValue("Failed", strconv.Itoa(result.Data.Failed))
			output.KeyValue("Duration", result.Data.Duration)
			return nil
		},
	}
	cmdutil.SetScope(cmdutil.ScopeFleet, cmd)
	return cmd
}

func newHistoryCmd() *cobra.Command {
	var opts struct {
		limit int
	}
	cmd := &cobra.Command{
		Use:          "history",
		Short:        "Get updater history",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			path := clitypes.UpdaterHistory(c.EnvID())
			if cmd.Flags().Changed("limit") {
				path = cmdutil.AppendQuery(path, url.Values{"limit": []string{strconv.Itoa(opts.limit)}})
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[[]types.AutoUpdateRecord]{
				Method:         http.MethodGet,
				Path:           path,
				FailureMessage: "failed to get updater history",
				Render: func(data []types.AutoUpdateRecord) error {
					headers := []string{"RESOURCE", "TYPE", "STATUS", "APPLIED", "STARTED"}
					rows := make([][]string, len(data))
					for i, h := range data {
						rows[i] = []string{
							cmp.Or(h.ResourceName, h.ResourceID),
							h.ResourceType,
							h.Status,
							strconv.FormatBool(h.UpdateApplied),
							h.StartTime.Format("2006-01-02 15:04"),
						}
					}

					return cmdutil.PrintTable(cmd, headers, rows, cmdutil.TableSummary{Resource: "history entries"})
				},
			})
		},
	}
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 50, "Number of history entries to show")
	return cmd
}
