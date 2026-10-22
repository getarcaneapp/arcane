package events

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

// Command is the parent command for event operations
func Command() *cobra.Command {
	command := &cobra.Command{
		Annotations: map[string]string{"arcane:scope": cmdutil.ScopeGlobal},
		Use:         "events",
		Aliases:     []string{"event", "evt"},
		Short:       "Manage events",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newDeleteCmd())
	command.AddCommand(newStatsCmd())
	command.AddCommand(newExportCmd())
	return command
}

type filterOptions struct {
	search    string
	severity  string
	eventType string
}

func (f *filterOptions) register(flags *pflag.FlagSet) {
	flags.StringVar(&f.search, "search", "", "Only events whose title, description, resource, or user matches")
	flags.StringVar(&f.severity, "severity", "", "Only events with these severities (comma-separated: info, success, warning, error)")
	flags.StringVar(&f.eventType, "type", "", "Only events of these types or categories (comma-separated, e.g. container, role.create, authz.denied)")
}

func (f filterOptions) query() url.Values {
	query := url.Values{}
	for key, value := range map[string]string{
		"search":   f.search,
		"severity": f.severity,
		"type":     f.eventType,
	} {
		if value != "" {
			query.Set(key, value)
		}
	}
	return query
}

func newListCmd() *cobra.Command {
	var opts struct {
		filterOptions

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
				Query:    opts.query(),
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
	opts.register(command.Flags())
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of events to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	command.Flags().BoolVar(&opts.envOnly, "environment", false, "List each environment's events instead of global events")
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

func newExportCmd() *cobra.Command {
	var opts struct {
		filterOptions

		environmentID string
		format        string
		outputFile    string
	}
	command := &cobra.Command{
		Use:          "export",
		Short:        "Export events as CSV or NDJSON, oldest first",
		Example:      "  arcane admin events export --type authz --format ndjson -o events.ndjson",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			query := opts.query()
			query.Set("format", opts.format)
			if opts.environmentID != "" {
				query.Set("environmentId", opts.environmentID)
			}
			return runExport(cmd, cmdutil.AppendQuery(clitypes.EventsExport(), query), opts.outputFile)
		},
	}
	opts.register(command.Flags())
	command.Flags().StringVar(&opts.environmentID, "environment-id", "", "Only events for this environment ID")
	cmdutil.EnumVarP(command, command.Flags(), &opts.format, "format", "", types.EventExportFormatCSV, "Export format",
		types.EventExportFormatCSV, types.EventExportFormatNDJSON)
	command.Flags().StringVarP(&opts.outputFile, "output-file", "o", "", "Write the export to this file instead of stdout")
	return command
}

func runExport(cmd *cobra.Command, path, outputFile string) error {
	c, err := cmdutil.ClientFromCommand(cmd)
	if err != nil {
		return err
	}
	resp, err := c.Get(cmd.Context(), path)
	if err != nil {
		return fmt.Errorf("failed to export events: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if statusErr := cmdutil.EnsureSuccessStatus(resp); statusErr != nil {
		return fmt.Errorf("failed to export events: %w", statusErr)
	}
	if outputFile == "" {
		_, copyErr := output.Copy(cmd.Context(), output.Stdout(), resp.Body, resp.ContentLength, "Exporting events", false)
		return copyErr
	}
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", outputFile, err)
	}
	written, copyErr := output.Copy(cmd.Context(), file, resp.Body, resp.ContentLength, "Exporting events", !cmdutil.JSONOutputEnabled(cmd))
	closeErr := file.Close()
	if writeErr := cmp.Or(copyErr, closeErr); writeErr != nil {
		return fmt.Errorf("failed to write event export: %w", writeErr)
	}
	if cmdutil.JSONOutputEnabled(cmd) {
		return cmdutil.PrintJSON(struct {
			Path  string `json:"path"`
			Bytes int64  `json:"bytes"`
		}{
			Path:  outputFile,
			Bytes: written,
		})
	}
	output.Success("Exported events (%d bytes) to %s", written, outputFile)
	return nil
}
