package updater

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/updater"
	"github.com/spf13/cobra"
)

var (
	jsonOutput   bool
	historyLimit int
)

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

		result, err := c.GetJSON[updater.Status](cmd.Context(), types.UpdaterStatus(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to get updater status")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Updater Status")
		output.KeyValue("Updating Containers", strconv.Itoa(result.Data.UpdatingContainers))
		output.KeyValue("Updating Projects", strconv.Itoa(result.Data.UpdatingProjects))
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

		// Updater run can take a long time as it pulls images and restarts containers
		c.SetTimeout(30 * time.Minute)

		result, err := c.PostJSON[updater.Result](cmd.Context(), types.UpdaterRun(c.EnvID()), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to run updater")
		}

		if jsonOutput {
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

// autoUpdateRecord mirrors the backend's models.AutoUpdateRecord, which is not
// exported through the shared types module.
type autoUpdateRecord struct {
	ID              string            `json:"id"`
	ResourceID      string            `json:"resourceId"`
	ResourceType    string            `json:"resourceType"`
	ResourceName    string            `json:"resourceName"`
	Status          string            `json:"status"`
	StartTime       time.Time         `json:"startTime"`
	EndTime         *time.Time        `json:"endTime,omitempty"`
	UpdateAvailable bool              `json:"updateAvailable"`
	UpdateApplied   bool              `json:"updateApplied"`
	Error           *string           `json:"error,omitempty"`
	OldImages       map[string]string `json:"oldImageVersions,omitempty"`
	NewImages       map[string]string `json:"newImageVersions,omitempty"`
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

		path := types.UpdaterHistory(c.EnvID())
		if cmd.Flags().Changed("limit") {
			path = cmdutil.AppendQuery(path, url.Values{"limit": []string{strconv.Itoa(historyLimit)}})
		}

		result, err := c.GetJSON[[]autoUpdateRecord](cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to get updater history")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		headers := []string{"RESOURCE", "TYPE", "STATUS", "APPLIED", "STARTED"}
		rows := make([][]string, len(result.Data))
		for i, h := range result.Data {
			name := h.ResourceName
			if name == "" {
				name = h.ResourceID
			}
			rows[i] = []string{
				name,
				h.ResourceType,
				h.Status,
				strconv.FormatBool(h.UpdateApplied),
				h.StartTime.Format("2006-01-02 15:04"),
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
	historyCmd.Flags().IntVarP(&historyLimit, "limit", "n", 50, "Number of history entries to show")
}
