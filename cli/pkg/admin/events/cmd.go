package events

import (
	"fmt"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/cli/v2/internal/client"
	"github.com/getarcaneapp/arcane/cli/v2/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v2/internal/output"
	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/event"
	"github.com/spf13/cobra"
)

var (
	limitFlag   int
	startFlag   int
	allFlag     bool
	forceFlag   bool
	jsonOutput  bool
	envOnlyFlag bool
)

// EventsCmd is the parent command for event operations
var EventsCmd = &cobra.Command{
	Use:     "events",
	Aliases: []string{"event", "evt"},
	Short:   "Manage events",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List events",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		endpoint := types.Events()
		if envOnlyFlag {
			endpoint = types.EventsEnvironment(c.EnvID())
		}

		return cmdutil.RunList(cmd, c, eventListSpecInternal(endpoint))
	},
}

// eventListSpecInternal builds the shared list spec for the global and
// per-environment event list commands, which render identically.
func eventListSpecInternal(endpoint string) cmdutil.ListSpec[event.Event] {
	return cmdutil.ListSpec[event.Event]{
		Resource: "events",
		Endpoint: endpoint,
		Params:   cmdutil.ListParams{Resource: "events", Limit: limitFlag, FallbackDefault: 20, Start: startFlag, All: allFlag},
		JSON:     jsonOutput,
		Headers:  []string{"ID", "TYPE", "RESOURCE", "USER", "TIMESTAMP"},
		Row: func(evt event.Event) []string {
			resource := ""
			if evt.ResourceName != nil && evt.ResourceType != nil {
				resource = fmt.Sprintf("%s (%s)", *evt.ResourceName, *evt.ResourceType)
			}
			username := ""
			if evt.Username != nil {
				username = *evt.Username
			}
			return []string{
				evt.ID,
				evt.Type,
				resource,
				username,
				evt.Timestamp.String(),
			}
		},
	}
}

var deleteCmd = &cobra.Command{
	Use:          "delete <event-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete event",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			confirmed, err := cmdutil.Confirm(cmd, fmt.Sprintf("Are you sure you want to delete event %s?", args[0]))
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		if _, err := c.DeleteJSON[base.MessageResponse](cmd.Context(), types.Event(args[0])); err != nil {
			return errors.WrapIf(err, "failed to delete event")
		}

		output.Success("Event deleted successfully")
		return nil
	},
}

var statsCmd = &cobra.Command{
	Use:          "stats",
	Short:        "Show global event counts by severity",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		// The severity counts DTO lives only in the backend's internal event
		// package, so mirror its wire shape here.
		result, err := c.GetJSON[struct {
			Total   int64 `json:"total"`
			Info    int64 `json:"info"`
			Success int64 `json:"success"`
			Warning int64 `json:"warning"`
			Error   int64 `json:"error"`
		}](cmd.Context(), types.EventsStats())
		if err != nil {
			return errors.WrapIf(err, "failed to get event stats")
		}

		if jsonOutput {
			return cmdutil.PrintJSON(result.Data)
		}

		output.Header("Event Stats")
		output.KeyValue("Total", result.Data.Total)
		output.KeyValue("Info", result.Data.Info)
		output.KeyValue("Success", result.Data.Success)
		output.KeyValue("Warning", result.Data.Warning)
		output.KeyValue("Error", result.Data.Error)
		return nil
	},
}

func init() {
	EventsCmd.AddCommand(listCmd)
	EventsCmd.AddCommand(deleteCmd)
	EventsCmd.AddCommand(statsCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of events to show")
	listCmd.Flags().IntVar(&startFlag, "start", 0, cmdutil.StartFlagUsage)
	listCmd.Flags().BoolVarP(&allFlag, "all", "a", false, cmdutil.AllFlagUsage)
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	listCmd.Flags().BoolVar(&envOnlyFlag, "environment", false, "Only list events for the current environment")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	statsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
