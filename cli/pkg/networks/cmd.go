package networks

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/network"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
)

// NetworksCmd is the parent command for network operations
var NetworksCmd = &cobra.Command{
	Use:     "networks",
	Aliases: []string{"network", "net", "n"},
	Short:   "Manage networks",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List networks",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Networks(c.EnvID())
		if limitFlag > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, limitFlag)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list networks: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[network.Summary]
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

		headers := []string{"ID", "NAME", "DRIVER", "SCOPE", "CREATED"}
		rows := make([][]string, len(result.Data))
		for i, net := range result.Data {
			rows[i] = []string{
				shortID(net.ID),
				net.Name,
				net.Driver,
				net.Scope,
				net.Created.Format("2006-01-02 15:04"),
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d networks\n", result.Pagination.TotalItems)
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <network-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a network",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			fmt.Printf("Are you sure you want to delete network %s? (y/N): ", shortID(args[0]))
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.Network(c.EnvID(), args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete network: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if jsonOutput {
			var result base.ApiResponse[interface{}]
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
				if resultBytes, err := json.MarshalIndent(result.Data, "", "  "); err == nil {
					fmt.Println(string(resultBytes))
				}
			}
			return nil
		}

		output.Success("Network %s deleted successfully", shortID(args[0]))
		return nil
	},
}

var countsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get network usage counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.NetworksCounts(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get network counts: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[network.UsageCounts]
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

		output.Header("Network Usage Counts")
		output.KeyValue("Total networks", result.Data.Total)
		output.KeyValue("In use", result.Data.Inuse)
		output.KeyValue("Unused", result.Data.Unused)
		return nil
	},
}

var pruneCmd = &cobra.Command{
	Use:          "prune",
	Short:        "Remove unused networks",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			fmt.Print("Are you sure you want to prune unused networks? (y/N): ")
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

		resp, err := c.Post(cmd.Context(), types.Endpoints.NetworksPrune(c.EnvID()), nil)
		if err != nil {
			return fmt.Errorf("failed to prune networks: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[network.PruneReport]
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

		output.Success("Networks pruned successfully")
		output.KeyValue("Deleted networks", len(result.Data.NetworksDeleted))
		return nil
	},
}

func init() {
	NetworksCmd.AddCommand(listCmd)
	NetworksCmd.AddCommand(deleteCmd)
	NetworksCmd.AddCommand(countsCmd)
	NetworksCmd.AddCommand(pruneCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of networks to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Prune command flags
	pruneCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force prune without confirmation")
	pruneCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Counts command flags
	countsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
