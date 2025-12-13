package imageupdates

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/imageupdate"
)

var jsonOutput bool

// ImageUpdatesCmd is the parent command for image update operations
var ImageUpdatesCmd = &cobra.Command{
	Use:     "image-updates",
	Aliases: []string{"imgup", "updates"},
	Short:   "Check for image updates",
}

var checkCmd = &cobra.Command{
	Use:          "check",
	Short:        "Check for image updates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.ImageUpdatesCheck(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to check updates: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[imageupdate.BatchResponse]
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

		output.Header("Image Update Check Results")
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

var checkAllCmd = &cobra.Command{
	Use:          "check-all",
	Short:        "Check all images for updates",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.Endpoints.ImageUpdatesCheckAll(c.EnvID()), nil)
		if err != nil {
			return fmt.Errorf("failed to check all updates: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[imageupdate.BatchResponse]
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
	Short:        "Check specific image for updates",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.ImageUpdatesCheckById(c.EnvID(), args[0]))
		if err != nil {
			return fmt.Errorf("failed to check image update: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[imageupdate.Response]
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

		output.Header("Image Update Status")
		output.KeyValue("Has Update", fmt.Sprintf("%t", result.Data.HasUpdate))
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

		resp, err := c.Get(cmd.Context(), types.Endpoints.ImageUpdatesSummary(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get summary: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[imageupdate.Summary]
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

		output.Header("Image Updates Summary")
		output.KeyValue("Total Images", fmt.Sprintf("%d", result.Data.TotalImages))
		output.KeyValue("Images with Updates", fmt.Sprintf("%d", result.Data.ImagesWithUpdates))
		output.KeyValue("Digest Updates", fmt.Sprintf("%d", result.Data.DigestUpdates))
		output.KeyValue("Errors", fmt.Sprintf("%d", result.Data.ErrorsCount))
		return nil
	},
}

func init() {
	ImageUpdatesCmd.AddCommand(checkCmd)
	ImageUpdatesCmd.AddCommand(checkAllCmd)
	ImageUpdatesCmd.AddCommand(checkImageCmd)
	ImageUpdatesCmd.AddCommand(summaryCmd)

	checkCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	checkAllCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	checkImageCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	summaryCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
