package containers

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/types"
)

var (
	containersLimit int
	containersAll   bool
)

// ContainersCmd is the parent command for container operations
var ContainersCmd = &cobra.Command{
	Use:     "containers",
	Aliases: []string{"container", "c"},
	Short:   "Manage containers",
}

var containersListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List containers",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.FormatContainers(c.EnvID())
		if containersLimit > 0 {
			path = fmt.Sprintf("%s?pageSize=%d", path, containersLimit)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}

		var result struct {
			Items []struct {
				ID     string   `json:"id"`
				Names  []string `json:"names"`
				Image  string   `json:"image"`
				State  string   `json:"state"`
				Status string   `json:"status"`
			} `json:"items"`
			Pagination struct {
				TotalItems int64 `json:"totalItems"`
			} `json:"pagination"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "ID\tNAME\tIMAGE\tSTATE\tSTATUS")
		for _, container := range result.Items {
			name := ""
			if len(container.Names) > 0 {
				name = container.Names[0]
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", shortID(container.ID), name, container.Image, container.State, container.Status)
		}
		_ = w.Flush()

		fmt.Printf("\nTotal: %d containers\n", result.Pagination.TotalItems)

		return nil
	},
}

func init() {
	ContainersCmd.AddCommand(containersListCmd)

	containersListCmd.Flags().IntVarP(&containersLimit, "limit", "n", 20, "Number of containers to show")
	containersListCmd.Flags().BoolVarP(&containersAll, "all", "a", false, "Show all containers (including stopped)")
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
