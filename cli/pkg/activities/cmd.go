// Package activities implements the `arcane activities` command group for
// tracking background operations (the web UI's activity center).
package activities

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v2/internal/types"
)

// Command is the parent command for background activity operations.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "activities",
		Aliases: []string{"activity", "act"},
		Short:   "Track background activities",
	}
	cmdutil.SetScope(cmdutil.ScopeResource, cmd)
	cmd.AddCommand(newListCmd(), newGetCmd(), newCancelCmd(), newClearCmd())
	return cmd
}

func newListCmd() *cobra.Command {
	var opts struct {
		limit        int
		start        int
		all          bool
		status       types.ActivityStatus
		activityType types.ActivityType
		resourceType string
	}
	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List background activities",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			query := url.Values{}
			if opts.status != "" {
				query.Set("status", string(opts.status))
			}
			if opts.activityType != "" {
				query.Set("type", string(opts.activityType))
			}
			if opts.resourceType != "" {
				query.Set("resourceType", opts.resourceType)
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.Activity]{
				Resource: "activities",
				Endpoint: clitypes.Activities(c.EnvID()),
				Params:   cmdutil.ListParams{Resource: "activities", Limit: opts.limit, FallbackDefault: 50, Start: opts.start, All: opts.all},
				Query:    query,
				Headers:  []string{"ID", "TYPE", "STATUS", "RESOURCE", "STARTED", "PROGRESS", "MESSAGE"},
				Row: func(item types.Activity) []string {
					resource := resourceLabel(item)
					progress := "-"
					if item.Progress != nil {
						progress = strconv.Itoa(*item.Progress) + "%"
					}
					return []string{
						item.ID,
						string(item.Type),
						string(item.Status),
						resource,
						item.StartedAt.Format(time.RFC3339),
						progress,
						cmp.Or(item.LatestMessage, item.Step, kit.FromPtr(item.Error)),
					}
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, cmd)
	cmd.Flags().IntVarP(&opts.limit, "limit", "n", 50, "Number of activities to show")
	cmd.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	cmd.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.status, "status", "", "", "Filter by status",
		types.ActivityStatusQueued, types.ActivityStatusRunning, types.ActivityStatusSuccess, types.ActivityStatusFailed, types.ActivityStatusCancelled)
	cmdutil.EnumVarP(cmd, cmd.Flags(), &opts.activityType, "type", "", "", "Filter by activity type",
		types.ActivityTypeJobRun, types.ActivityTypeImagePull, types.ActivityTypeImageBuild, types.ActivityTypeImageUpdateCheck,
		types.ActivityTypeProjectPull, types.ActivityTypeProjectBuild, types.ActivityTypeProjectDeploy, types.ActivityTypeProjectRedeploy,
		types.ActivityTypeProjectDown, types.ActivityTypeProjectRestart, types.ActivityTypeProjectDestroy, types.ActivityTypeContainerStart,
		types.ActivityTypeContainerStop, types.ActivityTypeContainerRestart, types.ActivityTypeContainerRedeploy, types.ActivityTypeContainerEdit,
		types.ActivityTypeContainerDelete, types.ActivityTypeContainerKill, types.ActivityTypeContainerPause, types.ActivityTypeContainerUnpause,
		types.ActivityTypeVulnerabilityScan, types.ActivityTypeImagePatch, types.ActivityTypeAutoUpdate, types.ActivityTypeSystemPrune,
		types.ActivityTypeResourceAction)
	cmd.Flags().StringVar(&opts.resourceType, "resource-type", "", "Filter by resource type")
	return cmd
}

func newGetCmd() *cobra.Command {
	var opts struct {
		messages int
	}
	cmd := &cobra.Command{
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
				clitypes.Activity(c.EnvID(), args[0]),
				url.Values{"limit": []string{strconv.Itoa(opts.messages)}},
			)
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ActivityDetail]{
				Method:         http.MethodGet,
				Path:           path,
				FailureMessage: "failed to get activity",
				Render: func(data types.ActivityDetail) error {
					item := data.Activity
					output.Header("Activity Details")
					output.KeyValue("ID", item.ID)
					output.KeyValue("Type", string(item.Type))
					output.KeyValue("Status", string(item.Status))
					resource := resourceLabel(item)
					if resource != "" {
						output.KeyValue("Resource", resource)
					}
					if item.Progress != nil {
						output.KeyValue("Progress", strconv.Itoa(*item.Progress)+"%")
					}
					if item.Step != "" {
						output.KeyValue("Step", item.Step)
					}
					if item.StartedBy != nil {
						output.KeyValue("Started By", cmp.Or(item.StartedBy.DisplayName, item.StartedBy.Username))
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

					if len(data.Messages) > 0 {
						output.Header("Messages")
						for _, msg := range data.Messages {
							_, _ = fmt.Fprintf(output.Stdout(), "%s [%s] %s\n", msg.CreatedAt.Format(time.RFC3339), msg.Level, msg.Message)
						}
					}
					return nil
				},
			})
		},
	}
	cmdutil.SetResource(activityRef, cmd)
	cmd.Flags().IntVar(&opts.messages, "messages", 500, "Maximum output messages to return")
	return cmd
}

func newCancelCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "cancel <activity-id>",
		Short:        "Cancel a running background activity",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			confirmed, confirmErr := cmdutil.ConfirmOrCancel(cmd, opts.force, fmt.Sprintf("Are you sure you want to cancel activity %s?", args[0]))
			if confirmErr != nil || !confirmed {
				return confirmErr
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.Activity]{
				Method:         http.MethodPost,
				Path:           clitypes.ActivityCancel(c.EnvID(), args[0]),
				FailureMessage: "failed to cancel activity",
				Render: func(data types.Activity) error {
					output.Success("Cancellation requested for activity %s (status: %s)", data.ID, data.Status)
					return nil
				},
			})
		},
	}
	cmdutil.SetResource(activityRef, cmd)
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Cancel without confirmation")
	return cmd
}

func newClearCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	cmd := &cobra.Command{
		Use:          "clear",
		Short:        "Clear finished activity history",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			confirmed, confirmErr := cmdutil.ConfirmOrCancel(cmd, opts.force, "Are you sure you want to clear the finished activity history?")
			if confirmErr != nil || !confirmed {
				return confirmErr
			}

			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.ActivityClearHistoryResult]{
				Method:         http.MethodDelete,
				Path:           clitypes.ActivitiesHistory(c.EnvID()),
				FailureMessage: "failed to clear activity history",
				Render: func(data types.ActivityClearHistoryResult) error {
					output.Success("Cleared %d finished activities", data.Deleted)
					return nil
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeFleet, cmd)
	cmd.Flags().BoolVarP(&opts.force, "force", "f", false, "Clear without confirmation")
	return cmd
}

var activityRef = cmdutil.ResourceRef[types.ActivityDetail, types.Activity]{
	EnvironmentScoped: true,
	Singular:          "activity", Plural: "activities",
	IDHint: "the identifier", ListCmd: "arcane-cli activities list",
	GetPath: clitypes.Activity, ListPath: clitypes.Activities,
	Matches: func(item types.Activity, lower, original string) bool {
		return strings.EqualFold(item.ID, original)
	},
	IdentifierOf: func(item types.ActivityDetail) string { return item.Activity.ID },
	Label:        func(item types.Activity) string { return item.ID },
	Promote:      func(item types.Activity) *types.ActivityDetail { return &types.ActivityDetail{Activity: item} },
}

func resourceLabel(item types.Activity) string {
	resource := cmp.Or(kit.FromPtr(item.ResourceName), kit.FromPtr(item.ResourceID))
	resourceType := kit.FromPtr(item.ResourceType)
	switch {
	case resourceType == "":
		return resource
	case resource == "":
		return resourceType
	default:
		return resourceType + "/" + resource
	}
}
