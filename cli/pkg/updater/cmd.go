package updater

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"getarcane.dev/cli/v3/internal/cmdutil"
	"getarcane.dev/cli/v3/internal/output"
	clitypes "getarcane.dev/cli/v3/internal/types"
	"getarcane.dev/types/v3"
)

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
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.UpdaterResult]{
				Method:         http.MethodPost,
				Path:           clitypes.UpdaterRun(c.EnvID()),
				FailureMessage: "failed to run updater",
				Timeout:        30 * time.Minute,
				Render:         RenderResult,
				Check:          CheckResult,
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeMergedAction, cmd)
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

// RenderResult prints the counts of an updater run.
func RenderResult(data types.UpdaterResult) error {
	output.Header("Updater Results")
	output.KeyValue("Checked", strconv.Itoa(data.Checked))
	output.KeyValue("Updated", strconv.Itoa(data.Updated))
	output.KeyValue("Skipped", strconv.Itoa(data.Skipped))
	output.KeyValue("Failed", strconv.Itoa(data.Failed))
	if data.Duration != "" {
		output.KeyValue("Duration", data.Duration)
	}
	return nil
}

// CheckResult fails an updater run that reports failed resources, which a successful response alone does not.
func CheckResult(data types.UpdaterResult) error {
	if data.Failed > 0 {
		return fmt.Errorf("updater failed for %d resources", data.Failed)
	}
	return nil
}
