package events

import (
	"fmt"
	"net/http"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the parent command for event operations
func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "events",
		Aliases: []string{"event", "evt"},
		Short:   "Manage events",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newDeleteCmd())
	command.AddCommand(newStatsCmd())
	return command
}

func newListCmd() *cobra.Command {
	var opts struct {
		limit   int
		start   int
		all     bool
		envOnly bool
	}
	command := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List events",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			endpoint := clitypes.Events()
			if opts.envOnly {
				endpoint = clitypes.EventsEnvironment(c.EnvID())
			}

			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.Event]{
				Resource: "events",
				Endpoint: endpoint,
				Params:   cmdutil.ListParams{Resource: "events", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Headers:  []string{"ID", "TYPE", "RESOURCE", "USER", "TIMESTAMP"},
				Row: func(evt types.Event) []string {
					resource := ""
					if evt.ResourceName != nil && evt.ResourceType != nil {
						resource = fmt.Sprintf("%s (%s)", *evt.ResourceName, *evt.ResourceType)
					}
					return []string{
						evt.ID,
						evt.Type,
						resource,
						kit.FromPtr(evt.Username),
						evt.Timestamp.String(),
					}
				},
			})
		},
	}
	cmdutil.SetScope(cmdutil.ScopeRead, command)
	command.Annotations["arcane:scope-flag"] = "environment"
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of events to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	command.Flags().BoolVar(&opts.envOnly, "environment", false, "Only list events for the current environment")
	return command
}

func newDeleteCmd() *cobra.Command {
	var opts struct {
		force bool
	}
	command := &cobra.Command{
		Use:          "delete <event-id>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Delete event",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunDelete(cmd, c, cmdutil.DeleteSpec{
				Force:          opts.force,
				Prompt:         fmt.Sprintf("Are you sure you want to delete event %s?", args[0]),
				Path:           clitypes.Event(args[0]),
				FailureMessage: "failed to delete event",
				SuccessMessage: "Event deleted successfully",
			})
		},
	}
	command.Flags().BoolVarP(&opts.force, "force", "f", false, "Force deletion without confirmation")
	return command
}

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "stats",
		Short:        "Show global event counts by severity",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}

			return cmdutil.Run(cmd, c, cmdutil.Spec[types.EventSeverityCounts]{
				Method:         http.MethodGet,
				Path:           clitypes.EventsStats(),
				FailureMessage: "failed to get event stats",
				Render: func(data types.EventSeverityCounts) error {
					output.Header("Event Stats")
					output.KeyValue("Total", data.Total)
					output.KeyValue("Info", data.Info)
					output.KeyValue("Success", data.Success)
					output.KeyValue("Warning", data.Warning)
					output.KeyValue("Error", data.Error)
					return nil
				},
			})
		},
	}
}
