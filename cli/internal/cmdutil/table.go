package cmdutil

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/cli/v3/internal/output"
)

type tableContextKey struct{}

type TableSummary struct {
	Resource  string
	Total     int64
	Paginated bool
}

type tableOutput struct {
	environment string
	headers     []string
	rows        [][]string
	summary     TableSummary
}

// PrintTable renders rows and an optional summary, collecting them during an environment preview.
func PrintTable(cmd *cobra.Command, headers []string, rows [][]string, summaries ...TableSummary) error {
	var summary TableSummary
	if len(summaries) > 0 {
		summary = summaries[0]
	}
	combined, _ := cmd.Context().Value(tableContextKey{}).(*tableOutput)
	formatted := make([][]string, len(rows))
	for i, row := range rows {
		formatted[i] = row
		if combined != nil {
			formatted[i] = slices.Concat([]string{combined.environment}, formatted[i])
		}
	}
	if combined != nil {
		if combined.headers == nil {
			combined.headers = slices.Concat([]string{"ENVIRONMENT"}, headers)
		}
		combined.rows = append(combined.rows, formatted...)
		combined.summary.Total = kit.Ternary(combined.summary.Total < 0 || summary.Total < 0, -1, combined.summary.Total+summary.Total)
		combined.summary.Resource, combined.summary.Paginated = summary.Resource, summary.Paginated
		return nil
	}
	if err := output.Table(headers, formatted); err != nil {
		return err
	}
	switch {
	case summary.Resource == "":
	case summary.Paginated && summary.Total < 0:
	case summary.Paginated:
		output.Showing(len(rows), summary.Total, summary.Resource)
	default:
		_, _ = fmt.Fprintf(output.Stdout(), "\nTotal: %d %s\n", len(rows), summary.Resource)
	}
	return nil
}
