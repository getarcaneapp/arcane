package volumes

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"go.getarcane.app/cli/internal/client"
	"go.getarcane.app/cli/internal/output"
	"go.getarcane.app/cli/internal/types"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/volume"
)

var (
	limitFlag  int
	forceFlag  bool
	jsonOutput bool
)

// VolumesCmd is the parent command for volume operations
var VolumesCmd = &cobra.Command{
	Use:     "volumes",
	Aliases: []string{"volume", "vol", "v"},
	Short:   "Manage volumes",
}

var listCmd = &cobra.Command{
	Use:          "list",
	Aliases:      []string{"ls"},
	Short:        "List volumes",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		path := types.Endpoints.Volumes(c.EnvID())
		if limitFlag > 0 {
			path = fmt.Sprintf("%s?limit=%d", path, limitFlag)
		}

		resp, err := c.Get(cmd.Context(), path)
		if err != nil {
			return fmt.Errorf("failed to list volumes: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.Paginated[volume.Volume]
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

		headers := []string{"NAME", "DRIVER", "MOUNTPOINT", "CREATED"}
		rows := make([][]string, len(result.Data))
		for i, vol := range result.Data {
			rows[i] = []string{
				vol.Name,
				vol.Driver,
				vol.Mountpoint,
				vol.CreatedAt,
			}
		}

		output.Table(headers, rows)
		fmt.Printf("\nTotal: %d volumes\n", result.Pagination.TotalItems)
		return nil
	},
}

var deleteCmd = &cobra.Command{
	Use:          "delete <volume-name>",
	Aliases:      []string{"rm", "remove"},
	Short:        "Delete a volume",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			fmt.Printf("Are you sure you want to delete volume %s? (y/N): ", args[0])
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

		resp, err := c.Delete(cmd.Context(), types.Endpoints.Volume(c.EnvID(), args[0]))
		if err != nil {
			return fmt.Errorf("failed to delete volume: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		output.Success("Volume %s deleted successfully", args[0])
		return nil
	},
}

var countsCmd = &cobra.Command{
	Use:          "counts",
	Short:        "Get volume usage counts",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.VolumesCounts(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get volume counts: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[interface{}]
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

		output.Header("Volume Usage Counts")
		resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal volume counts: %w", err)
		}
		fmt.Println(string(resultBytes))
		return nil
	},
}

var pruneCmd = &cobra.Command{
	Use:          "prune",
	Short:        "Remove unused volumes",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !forceFlag {
			fmt.Print("Are you sure you want to prune unused volumes? (y/N): ")
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

		resp, err := c.Post(cmd.Context(), types.Endpoints.VolumesPrune(c.EnvID()), nil)
		if err != nil {
			return fmt.Errorf("failed to prune volumes: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[interface{}]
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

		output.Success("Volumes pruned successfully")
		return nil
	},
}

var sizesCmd = &cobra.Command{
	Use:          "sizes",
	Short:        "Get volume sizes",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.VolumesSizes(c.EnvID()))
		if err != nil {
			return fmt.Errorf("failed to get volume sizes: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[interface{}]
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

		output.Header("Volume Sizes")
		resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal volume sizes: %w", err)
		}
		fmt.Println(string(resultBytes))
		return nil
	},
}

var usageCmd = &cobra.Command{
	Use:          "usage <volume-name>",
	Short:        "Get specific volume usage",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewFromConfig()
		if err != nil {
			return err
		}

		resp, err := c.Get(cmd.Context(), types.Endpoints.VolumeUsage(c.EnvID(), args[0]))
		if err != nil {
			return fmt.Errorf("failed to get volume usage: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var result base.ApiResponse[interface{}]
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

		output.Header("Volume Usage: %s", args[0])
		resultBytes, err := json.MarshalIndent(result.Data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal volume usage: %w", err)
		}
		fmt.Println(string(resultBytes))
		return nil
	},
}

func init() {
	VolumesCmd.AddCommand(listCmd)
	VolumesCmd.AddCommand(deleteCmd)
	VolumesCmd.AddCommand(countsCmd)
	VolumesCmd.AddCommand(pruneCmd)
	VolumesCmd.AddCommand(sizesCmd)
	VolumesCmd.AddCommand(usageCmd)

	// List command flags
	listCmd.Flags().IntVarP(&limitFlag, "limit", "n", 20, "Number of volumes to show")
	listCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Delete command flags
	deleteCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force deletion without confirmation")
	deleteCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Prune command flags
	pruneCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force prune without confirmation")
	pruneCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	// Other command flags
	countsCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	sizesCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	usageCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
}
