package updates

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
	"github.com/spf13/cobra"
)

var jsonOutput bool

// UpdatesCmd is the parent command for image update operations.
var UpdatesCmd = &cobra.Command{
	Use:   "updates",
	Short: "Check for image updates",
}

var checkAll bool

var checkCmd = &cobra.Command{
	Use:          "check [image-ref...]",
	Short:        "Check image references for updates",
	Long:         "Check image references for updates.\n\nWith a single reference the update status is checked directly; with multiple references a batch check is performed. With --all every image is checked.",
	Args:         cobra.ArbitraryArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		if checkAll {
			if len(args) > 0 {
				return errors.New("--all cannot be combined with image references")
			}
			// The handler declares a non-pointer Body, so an empty body is a 400.
			resp, err := c.Post(cmd.Context(), types.ImageUpdatesCheckAll(c.EnvID()), imageupdate.CheckAllImagesRequest{})
			if err != nil {
				return errors.WrapIf(err, "failed to check all updates")
			}
			return printBatchResults(resp, "failed to check all updates")
		}

		if len(args) == 0 {
			return errors.New("at least one image reference is required (or use --all)")
		}

		if len(args) > 1 {
			resp, err := c.Post(cmd.Context(), types.ImageUpdatesCheckBatch(c.EnvID()), imageupdate.BatchImageUpdateRequest{ImageRefs: args})
			if err != nil {
				return errors.WrapIf(err, "failed to check updates")
			}
			return printBatchResults(resp, "failed to check updates")
		}

		resp, err := c.Get(cmd.Context(), types.ImageUpdatesCheck(c.EnvID(), args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to check updates")
		}

		result, err := client.DecodeResponseStrict[imageupdate.Response](resp)
		if err != nil {
			return errors.WrapIf(err, "failed to check updates")
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Image Update Status")
		output.KeyValue("Image", args[0])
		output.KeyValue("Has Update", strconv.FormatBool(result.Data.HasUpdate))
		if result.Data.HasUpdate {
			output.KeyValue("Update Type", result.Data.UpdateType)
			output.KeyValue("Current Version", result.Data.CurrentVersion)
			output.KeyValue("Latest Version", result.Data.LatestVersion)
		}
		output.KeyValue("Check Time", result.Data.CheckTime.String())
		output.KeyValue("Response Time", fmt.Sprintf("%dms", result.Data.ResponseTimeMs))
		return nil
	},
}

var checkAllCmd = &cobra.Command{
	Use:          "check-all",
	Hidden:       true,
	Short:        "Check all images for updates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// The handler declares a non-pointer Body, so an empty body is a 400.
		resp, err := c.Post(cmd.Context(), types.ImageUpdatesCheckAll(c.EnvID()), imageupdate.CheckAllImagesRequest{})
		if err != nil {
			return errors.WrapIf(err, "failed to check all updates")
		}

		result, err := client.DecodeResponseStrict[imageupdate.BatchResponse](resp)
		if err != nil {
			return errors.WrapIf(err, "failed to check all updates")
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Check All Results")
		updatesAvailable := 0
		for imageRef, update := range result.Data {
			if update != nil && update.HasUpdate {
				output.KeyValue(imageRef, fmt.Sprintf("%s → %s (%s)", update.CurrentVersion, update.LatestVersion, update.UpdateType))
				updatesAvailable++
			}
		}

		fmt.Printf("\nTotal: %d images checked, %d updates available\n", len(result.Data), updatesAvailable)
		return nil
	},
}

var checkImageCmd = &cobra.Command{
	Use:          "check-image <image-id>",
	Hidden:       true,
	Short:        "Check specific image for updates",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.ImageUpdatesCheckById(c.EnvID(), args[0]))
		if err != nil {
			return errors.WrapIf(err, "failed to check image update")
		}

		result, err := client.DecodeResponseStrict[imageupdate.Response](resp)
		if err != nil {
			return errors.WrapIf(err, "failed to check image update")
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Image Update Status")
		output.KeyValue("Has Update", strconv.FormatBool(result.Data.HasUpdate))
		if result.Data.HasUpdate {
			output.KeyValue("Update Type", result.Data.UpdateType)
			output.KeyValue("Current Version", result.Data.CurrentVersion)
			output.KeyValue("Latest Version", result.Data.LatestVersion)
		}
		output.KeyValue("Check Time", result.Data.CheckTime.String())
		output.KeyValue("Response Time", fmt.Sprintf("%dms", result.Data.ResponseTimeMs))
		return nil
	},
}

var summaryCmd = &cobra.Command{
	Use:          "summary",
	Short:        "Get image updates summary",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.ImageUpdatesSummary(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to get summary")
		}

		result, err := client.DecodeResponseStrict[imageupdate.Summary](resp)
		if err != nil {
			return errors.WrapIf(err, "failed to get summary")
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return errors.WrapIf(err, "failed to marshal JSON")
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Header("Image Updates Summary")
		output.KeyValue("Total Images", strconv.Itoa(result.Data.TotalImages))
		output.KeyValue("Images with Updates", strconv.Itoa(result.Data.ImagesWithUpdates))
		output.KeyValue("Digest Updates", strconv.Itoa(result.Data.DigestUpdates))
		output.KeyValue("Errors", strconv.Itoa(result.Data.ErrorsCount))
		return nil
	},
}

// printBatchResults decodes a batch update-check response and prints every
// requested reference, including up-to-date and failed checks.
func printBatchResults(resp *http.Response, wrapMsg string) error {
	result, err := client.DecodeResponseStrict[imageupdate.BatchResponse](resp)
	if err != nil {
		return errors.WrapIf(err, wrapMsg)
	}

	if jsonOutput {
		resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
		if err != nil {
			return errors.WrapIf(err, "failed to marshal JSON")
		}
		fmt.Println(string(resultBytes))
		return nil
	}

	refs := make([]string, 0, len(result.Data))
	for imageRef := range result.Data {
		refs = append(refs, imageRef)
	}
	sort.Strings(refs)

	headers := []string{"IMAGE", "UPDATE", "CURRENT", "LATEST", "TYPE"}
	rows := make([][]string, 0, len(refs))
	updatesAvailable := 0
	for _, imageRef := range refs {
		update := result.Data[imageRef]
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

	output.Table(headers, rows)
	fmt.Printf("\nTotal: %d images checked, %d updates available\n", len(result.Data), updatesAvailable)
	return nil
}

func init() {
	UpdatesCmd.AddCommand(checkCmd)
	checkCmd.Flags().BoolVar(&checkAll, "all", false, "Check every image for updates")
	UpdatesCmd.AddCommand(checkAllCmd)
	UpdatesCmd.AddCommand(checkImageCmd)
	UpdatesCmd.AddCommand(summaryCmd)

	checkCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	checkAllCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	checkImageCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	summaryCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
