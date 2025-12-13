package events

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/event"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
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

		path := types.Endpoints.Events()
		if limitFlag > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, limitFlag)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list events: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[event.Event]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"ID", "TYPE", "RESOURCE", "USER", "TIMESTAMP"}
		rows := make([][]string, len(result.Data))
		for i, evt := range result.Data {
			resource := ""
			if evt.ResourceName != nil && evt.ResourceType != nil {
				resource = fmt.Sprintf("%s (%s)", *evt.ResourceName, *evt.ResourceType)
			}
			username := ""
			if evt.Username != nil {
				username = *evt.Username
			}
			rows[i] = []string{
				evt.ID,
				evt.Type,
				resource,
				username,
				evt.Timestamp.String(),
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d events\n", result.Pagination.TotalItems)
		return nil
	},
}

var listEnvCmd = &cobra.Command{
	Use:          "list-env",
	Short:        "List events for current environment",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.EventsEnvironment(c.EnvID())
		if limitFlag > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, limitFlag)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list environment events: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[event.Event]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if jsonOutput {
			resultBytes, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		headers := []string{"ID", "TYPE", "RESOURCE", "USER", "TIMESTAMP"}
		rows := make([][]string, len(result.Data))
		for i, evt := range result.Data {
			resource := ""
			if evt.ResourceName != nil && evt.ResourceType != nil {
				resource = fmt.Sprintf("%s (%s)", *evt.ResourceName, *evt.ResourceType)
			}
			username := ""
			if evt.Username != nil {
				username = *evt.Username
			}
			rows[i] = []string{
				evt.ID,
				evt.Type,
				resource,
				username,
				evt.Timestamp.String(),
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d events\n", result.Pagination.TotalItems)
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <event-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete event",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			fmt.Printf("Are you sure you want to delete event %s? (y/N): ", args[0])
			var response string
			if _, err := fmt.Scanln(&response); err != nil {
				fmt.Println("Cancelled")
				return nil
			}
			if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
				fmt.Println("Cancelled")
				return nil
			}
		}

		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Delete(cmd.Context(), types.Endpoints.Event(args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete event: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Event deleted successfully")
		return nil
	},
}

func init() {
	EventsCmd.AddCommand(listCmd)
	EventsCmd.AddCommand(listEnvCmd)
	EventsCmd.AddCommand(deleteCmd)

	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of events to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	listEnvCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of events to show")
	listEnvCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
