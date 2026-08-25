// Package activities implements the `arcane activities` command group for
// tracking background operations (the web UI's activity center).
package activities

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"emperror.dev/errors"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/spf13/cobra"
)

var (
	limitFlag        int
	startFlag        int
	allFlag          bool
	forceFlag        bool
	jsonOutput       bool
	statusFlag       string
	typeFlag         string
	resourceTypeFlag string
	messagesFlag     int
)

// ActivitiesCmd is the parent command for background activity operations.
var ActivitiesCmd = &cobra.Command{
	Use:     "activities",
	Aliases: []string{"activity", "act"},
	Short:   "Track background activities",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List background activities",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path, err := cmdutil.ApplyPaginationParams(cmd, types.Activities(c.EnvID()), cmdutil.ListParams{
			Resource:        "activities",
			Limit:           limitFlag,
			FallbackDefault: 50,
			Start:           startFlag,
			All:             allFlag,
		})
		if err != nil {
			return errors.WrapIf(err, "failed to build pagination query")
		}

		query := url.Values{}
		if statusFlag != "" {
			query.Set("status", statusFlag)
		}
		if typeFlag != "" {
			query.Set("type", typeFlag)
		}
		if resourceTypeFlag != "" {
			query.Set("resourceType", resourceTypeFlag)
		}
		if len(query) > 0 {
			path = cmdutil.AppendQuery(path, query)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to list activities")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to list activities")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.Paginated[activitytypes.Activity]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		headers := []string{"ID", "TYPE", "STATUS", "RESOURCE", "STARTED", "PROGRESS", "MESSAGE"}
		rows := make([][]string, len(result.Data))
		for i, item := range result.Data {
			rows[i] = []string{
				item.ID,
				string(item.Type),
				string(item.Status),
				activityResource(item),
				item.StartedAt.Format(time.RFC3339),
				activityProgress(item),
				activityMessage(item),
			}
		}

		output.Table(headers, rows)
		output.Showing(len(result.Data), result.Pagination.TotalItems, "activities")
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:          "get <activity-id>",
	Short:        "Get background activity details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path := cmdutil.AppendQuery(
			types.Activity(c.EnvID(), args[0]),
			url.Values{"limit": []string{strconv.Itoa(messagesFlag)}},
		)
		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return errors.WrapIf(err, "failed to get activity")
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := cmdutil.ReadJSONBody(resp)
		if err != nil {
			return errors.WrapIf(err, "failed to get activity")
		}

		if jsonOutput {
			return cmdutil.PrintRawJSON(body)
		}

		var result base.ApiResponse[activitytypes.Detail]
		if err := json.Unmarshal(body, &result); err != nil {
			return errors.WrapIf(err, "failed to parse response")
		}

		item := result.Data.Activity
		output.Header("Activity Details")
		output.KeyValue("ID", item.ID)
		output.KeyValue("Type", string(item.Type))
		output.KeyValue("Status", string(item.Status))
		if resource := activityResource(item); resource != "" {
			output.KeyValue("Resource", resource)
		}
		if item.Progress != nil {
			output.KeyValue("Progress", activityProgress(item))
		}
		if item.Step != "" {
			output.KeyValue("Step", item.Step)
		}
		if item.StartedBy != nil {
			startedBy := item.StartedBy.Username
			if item.StartedBy.DisplayName != "" {
				startedBy = item.StartedBy.DisplayName
			}
			output.KeyValue("Started By", startedBy)
		}
		output.KeyValue("Started", item.StartedAt.Format(time.RFC3339))
		if item.EndedAt != nil {
			output.KeyValue("Ended", item.EndedAt.Format(time.RFC3339))
		}
		if item.DurationMs != nil {
			output.KeyValue("Duration", (time.Duration(*item.DurationMs) * time.Millisecond).String())
		}
		if item.Error != nil && *item.Error != "" {
			output.KeyValue("Error", *item.Error)
		}

		if len(result.Data.Messages) > 0 {
			output.Header("Messages")
			for _, msg := range result.Data.Messages {
				fmt.Printf("%s [%s] %s\n", msg.CreatedAt.Format(time.RFC3339), msg.Level, msg.Message)
			}
		}
		return nil
	},
}

var cancelCmd = &cobra.Command{
	Use:          "cancel <activity-id>",
	Short:        "Cancel a running background activity",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to cancel activity %s?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Post(cmd.Context(), types.ActivityCancel(c.EnvID(), args[0]), nil)
		if err != nil {
			return errors.WrapIf(err, "failed to cancel activity")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[activitytypes.Activity]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return errors.WrapIf(err, "failed to cancel activity")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Cancellation requested for activity %s (status: %s)", result.Data.ID, result.Data.Status)
		return nil
	},
}

var clearCmd = &cobra.Command{
	Use:          "clear",
	Short:        "Clear finished activity history",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, "Are you sure you want to clear the finished activity history?")
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.ActivitiesHistory(c.EnvID()))
		if err != nil {
			return errors.WrapIf(err, "failed to clear activity history")
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[activitytypes.ClearHistoryResult]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return errors.WrapIf(err, "failed to clear activity history")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Success("Cleared %d finished activities", result.Data.Deleted)
		return nil
	},
}

func init() {
	ActivitiesCmd.AddCommand(listCmd)
	ActivitiesCmd.AddCommand(getCmd)
	ActivitiesCmd.AddCommand(cancelCmd)
	ActivitiesCmd.AddCommand(clearCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 50, "Number of activities to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	listCmd.Flags().StringVar(&statusFlag, "status", "", "Filter by status (queued, running, success, failed, cancelled)")
	listCmd.Flags().StringVar(&typeFlag, "type", "", "Filter by activity type (e.g. image_pull, project_deploy)")
	listCmd.Flags().StringVar(&resourceTypeFlag, "resource-type", "", "Filter by resource type")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Get command flags
	getCmd.Flags().IntVar(&messagesFlag, "messages", 500, "Maximum output messages to return")
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Cancel command flags
	cancelCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Cancel without confirmation")
	cancelCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Clear command flags
	clearCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Clear without confirmation")
	clearCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

func activityResource(item activitytypes.Activity) string {
	name := ""
	if item.ResourceName != nil {
		name = *item.ResourceName
	}
	if name == "" && item.ResourceID != nil {
		name = *item.ResourceID
	}
	if item.ResourceType != nil && *item.ResourceType != "" {
		if name == "" {
			return *item.ResourceType
		}
		return fmt.Sprintf("%s/%s", *item.ResourceType, name)
	}
	return name
}

func activityProgress(item activitytypes.Activity) string {
	if item.Progress == nil {
		return "-"
	}
	return strconv.Itoa(*item.Progress) + "%"
}

func activityMessage(item activitytypes.Activity) string {
	if item.LatestMessage != "" {
		return item.LatestMessage
	}
	if item.Step != "" {
		return item.Step
	}
	if item.Error != nil {
		return *item.Error
	}
	return ""
}
