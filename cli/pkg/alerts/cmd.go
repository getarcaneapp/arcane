package alerts

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/cli/internal/cmdutil"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	dashboardtypes "github.com/getarcaneapp/arcane/types/dashboard"
	"github.com/spf13/cobra"
)

var debugAllGood bool

// AlertsCmd shows dashboard alerts/action items.
var AlertsCmd = &cobra.Command{
	Use:          "alerts",
	Aliases:      []string{"alert", "actionitems"},
	Short:        "Show dashboard alerts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := cmdutil.ClientFromCommand(cmd)
		if err != nil {
			return err
		}

		path := types.Endpoints.DashboardActionItems(c.EnvID())
		if debugAllGood {
			parsed, err := url.Parse(path)
			if err != nil {
				return fmt.Errorf("failed to parse endpoint path: %w", err)
			}
			q := parsed.Query()
			q.Set("debugAllGood", "true")
			parsed.RawQuery = q.Encode()
			path = parsed.String()
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to get dashboard alerts: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[dashboardtypes.ActionItems]
		if err := cmdutil.DecodeJSON(resp, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if cmdutil.JSONOutputEnabled(cmd) {
			return cmdutil.PrintJSON(result.Data)
		}

		items := result.Data.Items
		if len(items) == 0 {
			output.Success("No alerts. Environment is good.")
			return nil
		}

		output.Header("Dashboard Alerts")
		headers := []string{"KIND", "COUNT", "SEVERITY"}
		rows := make([][]string, 0, len(items))
		for _, item := range items {
			rows = append(rows, []string{
				formatActionItemKind(item.Kind),
				fmt.Sprintf("%d", item.Count),
				strings.ToUpper(string(item.Severity)),
			})
		}
		output.Table(headers, rows)
		output.Info("Total: %d alert(s)", len(items))
		return nil
	},
}

func formatActionItemKind(kind dashboardtypes.ActionItemKind) string {
	switch kind {
	case dashboardtypes.ActionItemKindStoppedContainers:
		return "Stopped Containers"
	case dashboardtypes.ActionItemKindImageUpdates:
		return "Image Updates"
	case dashboardtypes.ActionItemKindActionableVulnerabilities:
		return "Actionable Vulnerabilities"
	case dashboardtypes.ActionItemKindExpiringKeys:
		return "Expiring Keys"
	default:
		value := strings.TrimSpace(string(kind))
		if value == "" {
			return "-"
		}
		return strings.ReplaceAll(value, "_", " ")
	}
}

func init() {
	AlertsCmd.Flags().Bool("json", false, "Output in JSON format")
	AlertsCmd.Flags().BoolVar(&debugAllGood, "debug-all-good", false, "Force no alerts (debug mode)")
}
