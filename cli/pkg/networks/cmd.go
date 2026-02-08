package networks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/getarcaneapp/arcane/cli/internal/client"
	"github.com/getarcaneapp/arcane/cli/internal/output"
	"github.com/getarcaneapp/arcane/cli/internal/prompt"
	"github.com/getarcaneapp/arcane/cli/internal/types"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/network"
	"github.com/spf13/cobra"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
)

const maxPromptOptions = 20

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

var getCmd = &cobra.Command{
	Use:          "get <network-id|name>",
	Short:        "Get network details",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		allowPrompt := !jsonOutput && prompt.IsInteractive()
		resolvedID, resolvedName, err := resolveNetworkID(cmd.Context(), c, args[0], allowPrompt)
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.Network(c.EnvID(), resolvedID))
		if err != nil {
			return fmt.Errorf("failed to get network: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[network.Inspect]
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

		display := resolvedName
		if display == "" {
			display = result.Data.Name
		}

		output.Header("Network Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", display)
		output.KeyValue("Driver", result.Data.Driver)
		output.KeyValue("Scope", result.Data.Scope)
		output.KeyValue("Created", result.Data.Created.Format("2006-01-02 15:04"))
		output.KeyValue("Internal", result.Data.Internal)
		output.KeyValue("Attachable", result.Data.Attachable)
		output.KeyValue("Ingress", result.Data.Ingress)
		output.KeyValue("Containers", len(result.Data.ContainersList))
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <network-id|name>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a network",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resolvedID, resolvedName, err := resolveNetworkID(cmd.Context(), c, args[0], false)
		if err != nil {
			return err
		}

		display := resolvedName
		if display == "" {
			display = shortID(resolvedID)
		}

		if !forceFlag {
			fmt.Printf("Are you sure you want to delete network %s? (y/N): ", display)
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.Network(c.EnvID(), resolvedID))
		if err != nil {
			return fmt.Errorf("failed to delete network: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if jsonOutput {
			var result base.ApiResponse[interface{}]
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(resultBytes))
			return nil
		}

		output.Success("Network %s deleted successfully", display)
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
	NetworksCmd.AddCommand(getCmd)
	NetworksCmd.AddCommand(deleteCmd)
	NetworksCmd.AddCommand(countsCmd)
	NetworksCmd.AddCommand(pruneCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of networks to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Get command flags
	getCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

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

func resolveNetworkID(ctx context.Context, c *client.Client, identifier string, allowPrompt bool) (string, string, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return "", "", fmt.Errorf("network identifier is required")
	}

	resp, err := c.Get(ctx, types.Endpoints.Network(c.EnvID(), trimmed))
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve network %q: %w", trimmed, err)
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return "", "", fmt.Errorf("failed to read network response: %w", err)
	}

	if resp.StatusCode == http.StatusOK {
		var result base.ApiResponse[network.Inspect]
		if err := json.Unmarshal(body, &result); err != nil {
			return "", "", fmt.Errorf("failed to parse network response: %w", err)
		}
		return result.Data.ID, result.Data.Name, nil
	}

	if resp.StatusCode != http.StatusNotFound {
		return "", "", fmt.Errorf("failed to resolve network %q (status %d): %s", trimmed, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	searchPath := fmt.Sprintf("%s?search=%s&limit=%d", types.Endpoints.Networks(c.EnvID()), url.QueryEscape(trimmed), 200)
	searchResp, err := c.Get(ctx, searchPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to search networks: %w", err)
	}

	searchBody, err := io.ReadAll(searchResp.Body)
	_ = searchResp.Body.Close()
	if err != nil {
		return "", "", fmt.Errorf("failed to read networks response: %w", err)
	}

	if searchResp.StatusCode < 200 || searchResp.StatusCode >= 300 {
		return "", "", fmt.Errorf("failed to search networks (status %d): %s", searchResp.StatusCode, strings.TrimSpace(string(searchBody)))
	}

	var result base.Paginated[network.Summary]
	if err := json.Unmarshal(searchBody, &result); err != nil {
		return "", "", fmt.Errorf("failed to parse networks response: %w", err)
	}

	identifierLower := strings.ToLower(trimmed)
	matches := make([]network.Summary, 0)
	for _, item := range result.Data {
		if networkMatches(item, identifierLower, trimmed) {
			matches = append(matches, item)
		}
	}

	if len(matches) == 1 {
		return matches[0].ID, matches[0].Name, nil
	}

	if len(matches) > 1 {
		if !allowPrompt {
			return "", "", fmt.Errorf("multiple networks match %q; use the network ID or run `arcane networks list`", trimmed)
		}
		if len(matches) > maxPromptOptions {
			return "", "", fmt.Errorf("multiple networks match %q (%d results); refine your query or use the network ID", trimmed, len(matches))
		}

		options := make([]string, 0, len(matches))
		for _, match := range matches {
			options = append(options, fmt.Sprintf("%s (%s)", match.Name, shortID(match.ID)))
		}
		choice, err := prompt.Select("network", options)
		if err != nil {
			return "", "", err
		}
		return matches[choice].ID, matches[choice].Name, nil
	}

	return "", "", fmt.Errorf("network %q not found; use the network ID or run `arcane networks list`", trimmed)
}

func networkMatches(item network.Summary, identifierLower, original string) bool {
	idLower := strings.ToLower(item.ID)
	if idLower == identifierLower || (len(identifierLower) >= 4 && strings.HasPrefix(idLower, identifierLower)) {
		return true
	}
	if strings.Contains(idLower, identifierLower) {
		return true
	}
	if strings.Contains(strings.ToLower(item.Name), identifierLower) {
		return true
	}
	return strings.EqualFold(item.Name, original)
}
