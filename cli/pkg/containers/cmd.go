package containers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/container"
)

var (
	containersLimit int
	containersAll   bool
	forceFlag       bool
	jsonOutput      bool
)

// ContainersCmd is the parent command for container operations
var ContainersCmd = &cobra.Command{
	Use:     "containers",
	Aliases: []string{"container", "c"},
	Short:   "Manage containers",
}

var containersListCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List containers",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Containers(c.EnvID())
		if containersLimit > 0 {
			path = fmt.Sprintf("%s?pageSize=%d", path, containersLimit)
		}
		if containersAll {
			separator := "?"
			if strings.Contains(path, "?") {
				separator = "&"
			}
			path = fmt.Sprintf("%s%sall=true", path, separator)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[container.Summary]
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

		headers := []string{"ID", "NAME", "IMAGE", "STATE", "STATUS"}
		rows := make([][]string, len(result.Data))
		for i, container := range result.Data {
			name := ""
			if len(container.Names) > 0 {
				name = strings.TrimPrefix(container.Names[0], "/")
			}
			rows[i] = []string{
				shortID(container.ID),
				name,
				container.Image,
				container.State,
				container.Status,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d containers\n", result.Pagination.TotalItems)
		return nil
	},
}

var containersGetCmd = &cobra.Command{
	Use:          "get <container-id>",
	Short:        "Get detailed container information",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Container(c.EnvID(), args[0])
		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to get container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.Details]
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

		output.Header("Container Details")
		output.KeyValue("ID", result.Data.ID)
		output.KeyValue("Name", result.Data.Name)
		output.KeyValue("Image", result.Data.Image)
		output.KeyValue("State", fmt.Sprintf("%s (Running: %v)", result.Data.State.Status, result.Data.State.Running))
		output.KeyValue("Created", result.Data.Created)
		return nil
	},
}

var containersStartCmd = &cobra.Command{
	Use:          "start <container-id>",
	Short:        "Start a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.ContainerStart(c.EnvID(), args[0])
		resp, err := c.Post(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.ActionResult]
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

		output.Success("Container %s started successfully", shortID(args[0]))
		return nil
	},
}

var containersStopCmd = &cobra.Command{
	Use:          "stop <container-id>",
	Short:        "Stop a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.ContainerStop(c.EnvID(), args[0])
		resp, err := c.Post(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.ActionResult]
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

		output.Success("Container %s stopped successfully", shortID(args[0]))
		return nil
	},
}

var containersRestartCmd = &cobra.Command{
	Use:          "restart <container-id>",
	Short:        "Restart a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.ContainerRestart(c.EnvID(), args[0])
		resp, err := c.Post(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("failed to restart container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.ActionResult]
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

		output.Success("Container %s restarted successfully", shortID(args[0]))
		return nil
	},
}

var containersUpdateCmd = &cobra.Command{
	Use:          "update <container-id>",
	Short:        "Update a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.ContainerUpdate(c.EnvID(), args[0])
		resp, err := c.Post(cmd.Context(), path, nil)
		if err != nil {
			return fmt.Errorf("failed to update container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.ActionResult]
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

		output.Success("Container %s updated successfully", shortID(args[0]))
		return nil
	},
}

var containersDeleteCmd = &cobra.Command{
	Use:          "delete <container-id>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a container",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			fmt.Printf("Are you sure you want to delete container %s? (y/N): ", shortID(args[0]))
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

		path := types.Endpoints.Container(c.EnvID(), args[0])
		resp, err := c.Delete(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to delete container: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.ActionResult]
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

		output.Success("Container %s deleted successfully", shortID(args[0]))
		return nil
	},
}

var containersCountsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get container status counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.ContainersCounts(c.EnvID())
		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to get container counts: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[container.StatusCounts]
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

		output.Header("Container Status Counts")
		output.KeyValue("Running", result.Data.RunningContainers)
		output.KeyValue("Stopped", result.Data.StoppedContainers)
		output.KeyValue("Total", result.Data.TotalContainers)
		return nil
	},
}

func init() {
	ContainersCmd.AddCommand(containersListCmd)
	ContainersCmd.AddCommand(containersGetCmd)
	ContainersCmd.AddCommand(containersStartCmd)
	ContainersCmd.AddCommand(containersStopCmd)
	ContainersCmd.AddCommand(containersRestartCmd)
	ContainersCmd.AddCommand(containersUpdateCmd)
	ContainersCmd.AddCommand(containersDeleteCmd)
	ContainersCmd.AddCommand(containersCountsCmd)

	// List command flags
	containersListCmd.Flags().IntVarP(&containersLimit, "limit", "n", 20, "Number of containers to show")
	containersListCmd.Flags().BoolVarP(&containersAll, "all", "a", false, "Show all containers (including stopped)")
	containersListCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	containersDeleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")

	// Global JSON output flags
	containersGetCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersStartCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersStopCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersRestartCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersUpdateCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersDeleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	containersCountsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
