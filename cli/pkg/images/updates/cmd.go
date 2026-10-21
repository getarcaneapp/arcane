package updates

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/runtime"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command builds the updates command tree.
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:   "updates",
		Short: "Check for image updates",
	}
	command.AddCommand(newCheckCmd(), newCheckAllCmd(), newCheckImageCmd(), newSummaryCmd())
	return command
}

func newCheckCmd() *cobra.Command {
	var opts struct {
		all bool
	}
	command := &cobra.Command{
		Annotations: map[string]string{"arcane:scope": cmdutil.ScopeFleet},
		Use:         "check [image-ref...]",
		Short:       "Check image references for updates",
		Long: "Check image references for updates.\n\nWith a single reference the update status is checked directly; with " +
			"multiple references a batch check is performed. With --all every image is checked.",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			if opts.all {
				if len(args) > 0 {
					return errors.New("--all cannot be combined with image references")
				}

				result, checkAllUpdatesErr := c.PostJSON[jsontext.Value](cmd.Context(), clitypes.ImageUpdatesCheckAction(c.EnvID(), "all"), struct{}{})
				if checkAllUpdatesErr != nil {
					return fmt.Errorf("failed to check all updates: %w", checkAllUpdatesErr)
				}
				return printBatchResults(cmd, result.Data)
			}

			if len(args) == 0 {
				return errors.New("at least one image reference is required (or use --all)")
			}

			if len(args) > 1 {
				result, checkBatchUpdatesErr := c.PostJSON[jsontext.Value](cmd.Context(), clitypes.ImageUpdatesCheckAction(c.EnvID(), "batch"), types.BatchImageUpdateRequest{ImageRefs: args})
				if checkBatchUpdatesErr != nil {
					return fmt.Errorf("failed to check updates: %w", checkBatchUpdatesErr)
				}
				return printBatchResults(cmd, result.Data)
			}

			result, err := c.GetJSON[jsontext.Value](cmd.Context(), clitypes.ImageUpdatesCheck(c.EnvID(), args[0]))
			if err != nil {
				return fmt.Errorf("failed to check updates: %w", err)
			}

			var data types.ImageUpdateResponse
			if decodeErr := json.Unmarshal(result.Data, &data); decodeErr != nil {
				return fmt.Errorf("failed to parse response: %w", decodeErr)
			}
			if app, ok := runtime.From(cmd.Context()); ok && app != nil && app.FleetConfirmed && data.Error != "" {
				return fmt.Errorf("failed to check updates: %s", data.Error)
			}
			if cmdutil.JSONOutputEnabled(cmd) {
				return cmdutil.PrintRawJSON(result.Data)
			}
			return printUpdateStatus(data, args[0])
		},
	}
	command.Flags().BoolVar(&opts.all, "all", false, "Check every image for updates")
	return command
}

func newCheckAllCmd() *cobra.Command {
	return &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeFleet},
		Use:          "check-all",
		Hidden:       true,
		Short:        "Check all images for updates",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			result, err := c.PostJSON[jsontext.Value](cmd.Context(), clitypes.ImageUpdatesCheckAction(c.EnvID(), "all"), struct{}{})
			if err != nil {
				return fmt.Errorf("failed to check all updates: %w", err)
			}

			return printBatchResults(cmd, result.Data)
		},
	}
}

func newCheckImageCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "check-image <image-id>",
		Hidden:       true,
		Short:        "Check specific image for updates",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ImageUpdateResponse]{
				Method:         http.MethodGet,
				Path:           clitypes.ImageUpdatesCheckById(c.EnvID(), args[0]),
				FailureMessage: "failed to check image update",
				Render:         func(data types.ImageUpdateResponse) error { return printUpdateStatus(data, "") },
			})
		},
	}
}

func newSummaryCmd() *cobra.Command {
	return &cobra.Command{
		Annotations:  map[string]string{"arcane:scope": cmdutil.ScopeRead},
		Use:          "summary",
		Short:        "Get image updates summary",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ImageUpdateSummary]{
				Method:         http.MethodGet,
				Path:           clitypes.ImageUpdatesSummary(c.EnvID()),
				FailureMessage: "failed to get summary",
				Render: func(data types.ImageUpdateSummary) error {
					output.Header("Image Updates Summary")
					output.KeyValue("Total Images", strconv.Itoa(data.TotalImages))
					output.KeyValue("Images with Updates", strconv.Itoa(data.ImagesWithUpdates))
					output.KeyValue("Digest Updates", strconv.Itoa(data.DigestUpdates))
					output.KeyValue("Errors", strconv.Itoa(data.ErrorsCount))
					return nil
				},
			})
		},
	}
}

// printBatchResults prints every requested reference, including
// up-to-date and failed checks.
func printBatchResults(cmd *cobra.Command, raw jsontext.Value) error {
	var result types.ImageUpdateBatchResponse
	if decodeErr := json.Unmarshal(raw, &result); decodeErr != nil {
		return fmt.Errorf("failed to parse response: %w", decodeErr)
	}
	if app, ok := runtime.From(cmd.Context()); ok && app != nil && app.FleetConfirmed {
		for ref, update := range result {
			if update == nil {
				return fmt.Errorf("image update check failed for %s: no result", ref)
			}
			if update.Error != "" {
				return fmt.Errorf("image update check failed for %s: %s", ref, update.Error)
			}
		}
	}
	if cmdutil.JSONOutputEnabled(cmd) {
		return cmdutil.PrintRawJSON(raw)
	}

	refs := slices.Sorted(maps.Keys(result))

	headers := []string{"IMAGE", "UPDATE", "CURRENT", "LATEST", "TYPE"}
	rows := make([][]string, 0, len(refs))
	updatesAvailable := 0
	for _, imageRef := range refs {
		update := result[imageRef]
		if update == nil {
			rows = append(rows, []string{imageRef, "unknown", "", "", ""})
			continue
		}
		if update.Error != "" {
			rows = append(rows, []string{imageRef, "error", update.CurrentVersion, "", update.Error})
			continue
		}
		if update.HasUpdate {
			updatesAvailable++
			rows = append(rows, []string{imageRef, "Yes", update.CurrentVersion, update.LatestVersion, update.UpdateType})
			continue
		}
		rows = append(rows, []string{imageRef, "No", update.CurrentVersion, "", ""})
	}

	if tableErr := cmdutil.PrintTable(cmd, headers, rows); tableErr != nil {
		return tableErr
	}
	_, _ = fmt.Fprintf(output.Stdout(), "\nTotal: %d images checked, %d updates available\n", len(result), updatesAvailable)
	return nil
}

func printUpdateStatus(data types.ImageUpdateResponse, imageRef string) error {
	output.Header("Image Update Status")
	if imageRef != "" {
		output.KeyValue("Image", imageRef)
	}
	output.KeyValue("Has Update", strconv.FormatBool(data.HasUpdate))
	if data.HasUpdate {
		output.KeyValue("Update Type", data.UpdateType)
		output.KeyValue("Current Version", data.CurrentVersion)
		output.KeyValue("Latest Version", data.LatestVersion)
	}
	output.KeyValue("Check Time", data.CheckTime.String())
	output.KeyValue("Response Time", fmt.Sprintf("%dms", data.ResponseTimeMs))
	return nil
}
