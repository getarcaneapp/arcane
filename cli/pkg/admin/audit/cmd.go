package audit

import (
	"cmp"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
	clitypes "github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func Command() *cobra.Command {
	command := &cobra.Command{
		Use:     "audit",
		Aliases: []string{"audit-logs"},
		Short:   "Inspect and export the audit log",
	}
	command.AddCommand(newListCmd())
	command.AddCommand(newGetCmd())
	command.AddCommand(newExportCmd())
	return command
}

type filterOptions struct {
	actor       string
	action      string
	outcome     string
	targetType  string
	environment string
	since       string
	until       string
}

func (f *filterOptions) register(flags *pflag.FlagSet) {
	flags.StringVar(&f.actor, "actor", "", "Only entries performed by this user ID")
	flags.StringVar(&f.action, "action", "", "Only entries with this action (e.g. role.create, authz.denied)")
	flags.StringVar(&f.outcome, "outcome", "", "Only entries with this outcome (success, denied, error)")
	flags.StringVar(&f.targetType, "target-type", "", "Only entries for this target type (e.g. role, binding, group)")
	flags.StringVar(&f.environment, "environment", "", "Only entries for this environment ID")
	flags.StringVar(&f.since, "since", "", "Only entries at or after this time (RFC 3339 or a duration ago, e.g. 24h)")
	flags.StringVar(&f.until, "until", "", "Only entries before this time (RFC 3339 or a duration ago, e.g. 1h)")
}

func (f filterOptions) query() (url.Values, error) {
	query := url.Values{}
	for key, value := range map[string]string{
		"actorUserId":   f.actor,
		"action":        f.action,
		"outcome":       f.outcome,
		"targetType":    f.targetType,
		"environmentId": f.environment,
	} {
		if value != "" {
			query.Set(key, value)
		}
	}
	for key, flag := range map[string]struct{ name, value string }{
		"from": {name: "since", value: f.since},
		"to":   {name: "until", value: f.until},
	} {
		if flag.value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, flag.value)
		if err != nil {
			ago, durationErr := time.ParseDuration(flag.value)
			if durationErr != nil {
				return nil, fmt.Errorf("invalid --%s %q (use RFC 3339 or a duration such as 24h)", flag.name, flag.value)
			}
			parsed = time.Now().Add(-ago)
		}
		query.Set(key, parsed.UTC().Format(time.RFC3339))
	}
	return query, nil
}

func newListCmd() *cobra.Command {
	var opts struct {
		filterOptions

		limit int
		start int
		all   bool
	}
	command := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List audit log entries",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := opts.query()
			if err != nil {
				return err
			}
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.RunList(cmd, c, cmdutil.ListSpec[types.AuditLog]{
				Resource: "audit entries",
				Endpoint: clitypes.AuditLogs(),
				Params:   cmdutil.ListParams{Resource: "audit", Limit: opts.limit, FallbackDefault: 20, Start: opts.start, All: opts.all},
				Query:    query,
				Headers:  []string{"ID", "TIME", "ACTOR", "ACTION", "OUTCOME", "TARGET", "ENVIRONMENT"},
				Row: func(entry types.AuditLog) []string {
					return []string{
						entry.ID,
						entry.CreatedAt.Format("2006-01-02 15:04:05"),
						cmp.Or(entry.ActorUsername, entry.ActorType),
						entry.Action,
						entry.Outcome,
						kit.Ternary(entry.TargetType == "", "", entry.TargetType+":"+cmp.Or(entry.TargetName, entry.TargetID)),
						kit.FromPtr(entry.EnvironmentID),
					}
				},
			})
		},
	}
	opts.register(command.Flags())
	command.Flags().IntVarP(&opts.limit, "limit", "n", 20, "Number of entries to show")
	command.Flags().IntVar(&opts.start, "start", 0, cmdutil.StartFlagUsage)
	command.Flags().BoolVarP(&opts.all, "all", "a", false, cmdutil.AllFlagUsage)
	return command
}

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "get <entry-id>",
		Short:        "Get an audit log entry",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := cmdutil.ClientFromCommand(cmd)
			if err != nil {
				return err
			}
			return cmdutil.Run(cmd, c, cmdutil.Spec[types.AuditLog]{
				Method:         http.MethodGet,
				Path:           clitypes.AuditLog(args[0]),
				FailureMessage: "failed to get audit entry",
				Render:         renderEntry,
			})
		},
	}
}

func renderEntry(entry types.AuditLog) error {
	output.Header("Audit Entry")
	output.KeyValue("ID", entry.ID)
	output.KeyValue("Time", entry.CreatedAt.Format(time.RFC3339))
	output.KeyValue("Actor", cmp.Or(entry.ActorUsername, kit.FromPtr(entry.ActorUserID))+" ("+entry.ActorType+")")
	output.KeyValue("Action", entry.Action)
	output.KeyValue("Outcome", entry.Outcome)
	for _, field := range []struct{ key, value string }{
		{key: "API key", value: kit.FromPtr(entry.ApiKeyID)},
		{key: "Target", value: kit.Ternary(entry.TargetType == "", "", entry.TargetType+":"+cmp.Or(entry.TargetName, entry.TargetID))},
		{key: "Environment", value: kit.FromPtr(entry.EnvironmentID)},
		{key: "Permission", value: entry.Permission},
		{key: "Request", value: kit.Ternary(entry.RequestPath == "", "", entry.RequestMethod+" "+entry.RequestPath)},
		{key: "Remote address", value: entry.RemoteAddr},
		{key: "User agent", value: entry.UserAgent},
	} {
		if field.value != "" {
			output.KeyValue(field.key, field.value)
		}
	}
	for _, section := range []struct {
		title string
		value any
	}{
		{title: "Before", value: entry.Before},
		{title: "After", value: entry.After},
		{title: "Metadata", value: kit.Ternary[any](len(entry.Metadata) == 0, nil, entry.Metadata)},
	} {
		if section.value == nil {
			continue
		}
		encoded, err := json.Marshal(section.value)
		if err != nil {
			return fmt.Errorf("failed to encode %s: %w", section.title, err)
		}
		output.Header("%s", section.title)
		if printErr := cmdutil.PrintRawJSON(encoded); printErr != nil {
			return printErr
		}
	}
	return nil
}

func newExportCmd() *cobra.Command {
	var opts struct {
		filterOptions

		format     string
		outputFile string
	}
	command := &cobra.Command{
		Use:          "export",
		Short:        "Export audit log entries as CSV or NDJSON",
		Example:      "  arcane admin audit export --format ndjson --since 168h -o audit.ndjson",
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := opts.query()
			if err != nil {
				return err
			}
			query.Set("format", opts.format)
			return runExport(cmd, cmdutil.AppendQuery(clitypes.AuditLogsExport(), query), opts.outputFile)
		},
	}
	opts.register(command.Flags())
	cmdutil.EnumVarP(command, command.Flags(), &opts.format, "format", "", types.AuditExportFormatCSV, "Export format",
		types.AuditExportFormatCSV, types.AuditExportFormatNDJSON)
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
		return fmt.Errorf("failed to export audit log: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if statusErr := cmdutil.EnsureSuccessStatus(resp); statusErr != nil {
		return fmt.Errorf("failed to export audit log: %w", statusErr)
	}
	if outputFile == "" {
		_, copyErr := output.Copy(cmd.Context(), output.Stdout(), resp.Body, resp.ContentLength, "Exporting audit log", false)
		return copyErr
	}
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", outputFile, err)
	}
	written, copyErr := output.Copy(cmd.Context(), file, resp.Body, resp.ContentLength, "Exporting audit log", !cmdutil.JSONOutputEnabled(cmd))
	closeErr := file.Close()
	if writeErr := cmp.Or(copyErr, closeErr); writeErr != nil {
		return fmt.Errorf("failed to write audit export: %w", writeErr)
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
	output.Success("Exported audit log (%d bytes) to %s", written, outputFile)
	return nil
}
